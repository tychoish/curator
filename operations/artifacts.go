package operations

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/pkg/errors"
	"github.com/tychoish/bond"
	"github.com/tychoish/bond/recall"
	"github.com/urfave/cli"
	"golang.org/x/net/context"
)

func Artifacts() cli.Command {
	var target string
	var arch string

	if runtime.GOOS == "darwin" {
		target = "osx"
	} else {
		target = runtime.GOOS
	}

	if runtime.GOARCH == "amd64" {
		arch = "x86_64"
	} else if runtime.GOARCH == "386" {
		arch = "i686"
	} else if runtime.GOARCH == "arm" {
		arch = "arm64"
	} else {
		arch = runtime.GOARCH
	}

	return cli.Command{
		Name:    "artifacts",
		Aliases: []string{"archives", "build"},
		Usage:   "download ",
		Subcommands: []cli.Command{
			cli.Command{
				Name:    "download",
				Usage:   "downloads builds of MongoDB",
				Aliases: []string{"dl", "get"},
				Flags: baseDlFlags(true,
					cli.StringFlag{
						Name:  "timeout",
						Value: "no-timeout",
						Usage: "maximum duration for operation, defaults to no time out",
					},
					cli.StringFlag{
						Name:  "target",
						Value: target,
						Usage: "name of target platform or operating system",
					},
					cli.StringFlag{
						Name:  "arch",
						Value: arch,
						Usage: "name of target architecture",
					},
					cli.StringFlag{
						Name:  "edition",
						Value: "base",
						Usage: "name of build edition",
					},
					cli.BoolFlag{
						Name:  "debug",
						Usage: "specify to download debug symbols",
					}),
				Action: func(c *cli.Context) error {
					var cancel context.CancelFunc
					ctx := context.Background()

					timeout := c.String("timeout")
					if timeout != "no-timeout" {
						ttl, err := time.ParseDuration(timeout)
						if err != nil {
							return errors.Wrapf(err, "%s is not a valid timeout", timeout)
						}
						ctx, cancel = context.WithTimeout(ctx, ttl)
						defer cancel()
					} else {
						ctx, cancel = context.WithCancel(ctx)
						defer cancel()
					}

					opts := bond.BuildOptions{
						Target:  c.String("target"),
						Arch:    bond.MongoDBArch(c.String("arch")),
						Edition: bond.MongoDBEdition(c.String("edition")),
						Debug:   c.Bool("debug"),
					}

					err := recall.FetchReleases(ctx, c.StringSlice("version"), c.String("path"), opts)
					if err != nil {
						return errors.Wrap(err, "problem fetching releases")
					}

					return nil
				},
			},
			cli.Command{
				Name:  "list-all",
				Usage: "find all targets, editions and architectures for a version",
				Flags: baseDlFlags(false),
				Action: func(c *cli.Context) error {
					version, err := getVersionForListing(c.String("version"), c.String("path"))
					if err != nil {
						return errors.Wrap(err, "problem fetching version")
					}

					fmt.Println(version.GetBuildTypes())
					return nil
				},
			},
			cli.Command{
				Name:  "list-map",
				Usage: "find targets/edition/architecture mappings for a version",
				Flags: baseDlFlags(false),
				Action: func(c *cli.Context) error {
					version, err := getVersionForListing(c.String("version"), c.String("path"))
					if err != nil {
						return errors.Wrap(err, "problem fetching version")
					}

					fmt.Println(version)
					return nil
				},
			},
		},
	}
}

func baseDlFlags(versionSlice bool, flags ...cli.Flag) []cli.Flag {
	if versionSlice {
		flags = append(flags,
			cli.StringSliceFlag{
				Name:  "version",
				Usage: "specify a version (may specify multiple times)",
			})
	} else {
		flags = append(flags,
			cli.StringFlag{
				Name:  "version",
				Usage: "specify a version (may specify multiple times)",
			})
	}

	return append(flags,
		cli.StringFlag{
			Name:   "path",
			EnvVar: "CURATOR_ARTIFACTS_DIRECTORY",
			Value:  filepath.Join(os.TempDir(), "curator-artifact-cache"),
			Usage:  "path to top level of cache directory",
		})
}

func getVersionForListing(release, path string) (*bond.ArtifactVersion, error) {
	feed, err := bond.GetArtifactsFeed(path)
	if err != nil {
		return nil, errors.Wrap(err, "problem fetching artifacts feed")
	}

	version, ok := feed.GetVersion(release)
	if !ok {
		return nil, errors.Errorf("no version for %s", release)
	}

	return version, nil
}
