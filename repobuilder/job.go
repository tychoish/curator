package repobuilder

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/goamz/goamz/s3"
	"github.com/mongodb/amboy"
	"github.com/mongodb/amboy/dependency"
	"github.com/mongodb/curator"
	"github.com/mongodb/curator/sthree"
	"github.com/tychoish/grip"
)

type jobImpl interface {
	rebuildRepo(string, *grip.MultiCatcher, *sync.WaitGroup)
	injectNewPackages(string) ([]string, error)
}

type Job struct {
	Name         string                `bson:"name" json:"name" yaml:"name"`
	Distro       *RepositoryDefinition `bson:"distro" json:"distro" yaml:"distro"`
	Conf         *RepositoryConfig     `bson:"conf" json:"conf" yaml:"conf"`
	DryRun       bool                  `bson:"dry_run" json:"dry_run" yaml:"dry_run"`
	IsComplete   bool                  `bson:"completed" json:"completed" yaml:"completed"`
	Output       map[string]string     `bson:"output" json:"output" yaml:"output"`
	JobType      amboy.JobType         `bson:"job_type" json:"job_type" yaml:"job_type"`
	D            dependency.Manager    `bson:"dependency" json:"dependency" yaml:"dependency"`
	Version      string                `bson:"version" json:"version" yaml:"version"`
	Arch         string                `bson:"arch" json:"arch" yaml:"arch"`
	Profile      string                `bson:"aws_profile" json:"aws_profile" yaml:"aws_profile"`
	WorkSpace    string                `bson:"local_workdir" json:"local_workdir" yaml:"local_workdir"`
	PackagePaths []string              `bson:"package_paths" json:"package_paths" yaml:"package_paths"`
	workingDirs  []string
	release      *curator.MongoDBVersion
	mutex        sync.Mutex
}

func buildRepoJob() *Job {
	return &Job{
		D:      dependency.NewAlways(),
		Output: make(map[string]string),
		JobType: amboy.JobType{
			Name:    "build-repo",
			Version: 0,
		},
	}
}

func (j *Job) ID() string {
	return j.Name
}

func (j *Job) Completed() bool {
	return j.IsComplete
}

func (j *Job) Type() amboy.JobType {
	return j.JobType
}

func (j *Job) Dependency() dependency.Manager {
	return j.D
}

func (j *Job) SetDependency(d dependency.Manager) {
	if d.Type().Name == dependency.AlwaysRun {
		j.D = d
	} else {
		grip.Warning("repo building jobs should take 'always'-run dependencies.")
	}
}

func (j *Job) markComplete() {
	j.IsComplete = true
}

func (j *Job) linkPackages(dest string) error {
	catcher := grip.NewCatcher()
	wg := &sync.WaitGroup{}
	defer wg.Wait()
	for _, pkg := range j.PackagePaths {
		mirror := filepath.Join(dest, filepath.Base(pkg))
		if _, err := os.Stat(mirror); os.IsNotExist(err) {
			grip.Noticeln("creating directory:", dest)
			catcher.Add(os.MkdirAll(dest, 0744))

			grip.Infof("copying package %s to local staging %s", pkg, dest)

			err = os.Link(pkg, mirror)
			if err != nil {
				catcher.Add(err)
				grip.Error(err)
				continue
			}

			if j.Distro.Type == RPM {
				wg.Add(1)
				go func(toSign string) {
					catcher.Add(j.signFile(toSign, true))
					wg.Done()
				}(mirror)
			}

		} else {
			grip.Infof("file %s is already mirrored", mirror)
		}
	}

	return catcher.Resolve()
}

func (j *Job) signFile(fileName string, overwrite bool) error {
	// In the future it would be nice if we could talk to the
	// notary service directly rather than shelling out here. The
	// final option controls if we overwrite this file.

	var keyName string

	if j.Distro.Type == DEB && j.release.Series() == "3.0" {
		keyName = "richard"
	} else {
		keyName = "server-" + j.release.StableReleaseSeries()
	}

	token := os.Getenv("NOTARY_TOKEN")
	if token == "" {
		return errors.New(fmt.Sprintln("the notary service auth token",
			"(NOTARY_TOKEN) is not defined in the environment"))
	}

	extension := "gpg"

	if !overwrite {
		// if we're not overwriting the unsigned source file
		// with the signed file, then we should remove the
		// signed artifact before. Unclear if this is needed,
		// the cronjob did this.
		_ = os.Remove(fileName + "." + extension)
	}

	args := []string{
		"notary-client.py",
		"--key-name", keyName,
		"--auth-token", token,
		"--comment", "\"curator package signing\"",
		"--notary-url", j.Conf.Services.NotaryURL,
		"--archive-file-ext", extension,
		"--outputs", "sig",
	}

	if overwrite {
		grip.Noticef("overwriting existing contents of file '%s' while signing it", fileName)
		args = append(args, "--package-file-suffix", "\"\"")
	}

	args = append(args, filepath.Base(fileName))
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = filepath.Dir(fileName)

	grip.Infoln("running notary command:", strings.Replace(
		strings.Join(cmd.Args, " "),
		token, "XXXXX", -1))

	out, err := cmd.CombinedOutput()
	output := string(out)

	if err != nil {
		grip.Warningf("error signed file '%s': %s", fileName, err.Error())
	} else {
		grip.Noticeln("successfully signed file:", fileName)
	}

	grip.Debugln("notary-client.py output:", output)

	j.mutex.Lock()
	defer j.mutex.Unlock()
	j.Output["sign-"+fileName] = output

	return err
}

func (j *Job) Run() error {
	bucket := sthree.GetBucketWithProfile(j.Distro.Bucket, j.Profile)
	bucket.NewFilePermission = s3.PublicRead

	err := bucket.Open()
	defer bucket.Close()
	if err != nil {
		return err
	}

	defer j.markComplete()
	wg := &sync.WaitGroup{}
	catcher := grip.NewCatcher()

	for _, remote := range j.Distro.Repos {
		wg.Add(1)
		go func(repo *RepositoryDefinition, workSpace, remote string) {
			grip.Infof("rebuilding %s.%s", bucket, remote)
			defer wg.Done()

			local := filepath.Join(workSpace, remote)

			var err error
			j.workingDirs = append(j.workingDirs, local)

			err = os.MkdirAll(local, 0755)
			if err != nil {
				catcher.Add(err)
				return
			}

			if j.DryRun {
				grip.Noticef("in dry run mode. would download from %s to %s", remote, local)
			} else {
				grip.Infof("downloading from %s to %s", remote, local)
				err = bucket.SyncFrom(local, remote)
				if err != nil {
					catcher.Add(err)
					return
				}
			}

			var changedRepos []string
			if j.DryRun {
				grip.Noticef("in dry run mode. would link packages [%s] to %s",
					strings.Join(j.PackagePaths, "; "), local)
			} else {
				grip.Info("copying new packages into local staging area")
				changedRepos, err = injectNewPackages(j, local)
				if err != nil {
					catcher.Add(err)
					return
				}
			}

			rWg := &sync.WaitGroup{}
			rCatcher := grip.NewCatcher()
			for _, dir := range changedRepos {
				rWg.Add(1)
				go rebuildRepo(j, dir, rCatcher, rWg)
			}
			rWg.Wait()

			if rCatcher.HasErrors() {
				grip.Errorf("encountered error rebuilding %s (%s). Uploading no data",
					remote, local)
				catcher.Add(rCatcher.Resolve())
				return
			}

			if j.DryRun {
				grip.Noticef("in dry run mode. otherwise would have built %s (%s)",
					remote, local)
			} else {
				// don't need to return early here, only
				// because this is the last operation.
				catcher.Add(bucket.SyncTo(local, remote))
				grip.Noticef("completed rebuilding repo %s (%s)", remote, local)
			}
		}(j.Distro, j.WorkSpace, remote)
	}
	wg.Wait()

	grip.Notice("completed rebuilding all repositories")
	return catcher.Resolve()
}

// shim methods so that we can reuse the Run() method from
// repobuilder.Job for all types
func injectNewPackages(j interface{}, local string) ([]string, error) {
	switch j := j.(type) {
	case *Job:
		if j.Type().Name == "build-deb-repo" {
			job := BuildDEBRepoJob{*j}
			return job.injectNewPackages(local)
		} else if j.Type().Name == "build-rpm-repo" {
			job := BuildRPMRepoJob{*j}
			return job.injectNewPackages(local)
		} else {
			return []string{}, fmt.Errorf("builder %s is not supported", j.Type().Name)
		}
	default:
		return []string{}, fmt.Errorf("type %T is not supported", j)
	}
}

func rebuildRepo(j interface{}, workingDir string, catcher *grip.MultiCatcher, wg *sync.WaitGroup) {
	grip.Infoln("rebuilding repo:", workingDir)

	switch j := j.(type) {
	case *Job:
		if j.Type().Name == "build-deb-repo" {
			job := BuildDEBRepoJob{*j}
			job.rebuildRepo(workingDir, catcher, wg)
		} else if j.Type().Name == "build-rpm-repo" {
			job := BuildRPMRepoJob{*j}
			job.rebuildRepo(workingDir, catcher, wg)
		} else {
			e := fmt.Sprintf("builder %s is not supported", j.Type().Name)
			grip.Error(e)
			catcher.Add(errors.New(e))
		}
	default:
		e := fmt.Sprintf("cannot build repo for type: %T", j)
		grip.Error(e)
		catcher.Add(errors.New(e))
	}
}
