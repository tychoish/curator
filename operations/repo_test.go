package operations

import (
	"os"

	"github.com/urfave/cli"
)

func (s *CommandsSuite) TestRepoFlags() {
	flags := repoFlags()

	names := make(map[string]bool)
	for _, flag := range flags {
		names[flag.GetName()] = true

		if flag.GetName() == "dry-run" {
			s.IsType(cli.BoolFlag{}, flag)
		} else {
			s.IsType(cli.StringFlag{}, flag)
		}
	}

	s.Len(names, 9)
	s.Len(flags, 9)
	s.True(names["config"])
	s.True(names["distro"])
	s.True(names["version"])
	s.True(names["edition"])
	s.True(names["arch"])
	s.True(names["packages"])
	s.True(names["profile"])
	s.True(names["dry-run"])
}

func (s *CommandsSuite) TestDryRunOperationOnProcess() {
	err := buildRepo(
		"./", // packages
		"../repobuilder/config_test.yaml", // repo config path
		"./",         // workingdir
		"rhel7",      // distro
		"enterprise", // edition
		"2.8.0",      // mongodbe version
		"x86_64",     // arch
		"default",    // aws profile
		true)         // dryrun

	s.Equal(err.Error(), "no packages found in path './'")
}

func (s *CommandsSuite) TestGetPackagesFunction() {
	cwd, err := os.Getwd()
	s.NoError(err)

	testFiles, err := getPackages(cwd, "_test.go")
	s.NoError(err)
	s.Len(testFiles, 3)

	goFiles, err := getPackages(cwd, ".go")
	s.NoError(err)
	s.Len(goFiles, 1+len(testFiles)*2)

	noFiles, err := getPackages(cwd+".DOES_NOT_EXIST", "foo")
	s.Error(err)
	s.Len(noFiles, 0)
}
