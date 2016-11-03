package main

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/pkg/errors"
	"github.com/tychoish/grip"
	"github.com/tychoish/grip/level"
	"github.com/urfave/cli"
)

// inspired by https://gist.github.com/jonmorehouse/9060515

func addFile(tw *tar.Writer, prefix string, unit archiveWorkUnit) error {
	file, err := os.Open(unit.path)
	if err != nil {
		return err
	}
	defer file.Close()
	// now lets create the header as needed for this file within the tarball
	header := new(tar.Header)
	header.Name = filepath.Join(prefix, unit.path)
	header.Size = unit.stat.Size()
	header.Mode = int64(unit.stat.Mode())
	header.ModTime = unit.stat.ModTime()
	// write the header to the tarball archive
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	// copy the file data to the tarball
	if _, err := io.Copy(tw, file); err != nil {
		return err
	}

	grip.Infof("added %s to archive", header.Name)
	return nil
}

type archiveWorkUnit struct {
	path string
	stat os.FileInfo
}

func getContents(paths []string, exclusions []string) <-chan archiveWorkUnit {
	output := make(chan archiveWorkUnit, 100)

	var matchers []*regexp.Regexp
	for _, pattern := range exclusions {
		matchers = append(matchers, regexp.MustCompile(pattern))
	}

	go func() {
		for _, path := range paths {
			err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}

				if info.IsDir() {
					return nil
				}

				for _, exclude := range matchers {
					if exclude.MatchString(p) {
						return nil
					}
				}

				output <- archiveWorkUnit{
					path: p,
					stat: info,
				}
				return nil
			})

			if err != nil {
				grip.CatchErrorPanic(err)
			}
		}
		close(output)
	}()

	return output
}

func makeTarball(fileName, prefix string, paths []string, exclude []string) error {
	// set up the output file
	file, err := os.Create(fileName)
	if err != nil {
		return errors.Wrapf(err, "problem creating file %s", fileName)
	}
	defer file.Close()

	// set up the gzip writer
	gw := gzip.NewWriter(file)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	grip.Infof("creating archive %s", fileName)
	for unit := range getContents(paths, exclude) {
		err := addFile(tw, prefix, unit)

		if err != nil {
			return errors.Wrapf(err, "error adding path: %s [%+v]",
				unit.path, unit)
		}
	}

	return nil
}

func main() {
	app := cli.NewApp()
	app.Name = "make-tarball"
	app.Usage = "create a tarball"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name: "name",
		},
		cli.StringFlag{
			Name: "prefix",
		},
		cli.StringSliceFlag{
			Name: "item",
		},
		cli.StringSliceFlag{
			Name: "exclude",
		},
	}

	grip.SetName("make-tarball")
	grip.SetThreshold(level.Info)
	grip.UseNativeLogger()

	app.Action = func(c *cli.Context) error {
		return makeTarball(c.String("name"), c.String("prefix"),
			c.StringSlice("item"), c.StringSlice("exclude"))
	}

	grip.CatchErrorFatal(app.Run(os.Args))
}
