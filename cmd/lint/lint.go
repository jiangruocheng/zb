package lint

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/urfave/cli"
	"jrubin.io/zb/cmd"
	"jrubin.io/zb/lib/project"
	"jrubin.io/zb/lib/zbcontext"
	"jrubin.io/zb/lib/zblint"
)

// Cmd is the lint command
var Cmd cmd.Constructor = &cc{}

type cc struct {
	zblint.ZBLint
}

func (cmd *cc) New(_ *cli.App, config *cmd.Config) cli.Command {
	cmd.Config = config

	return cli.Command{
		Name:      "lint",
		Usage:     "gometalinter with cache and better defaults",
		ArgsUsage: "[arguments] [packages]",
		Before: func(c *cli.Context) error {
			cmd.LintSetup()
			return nil
		},
		Action: func(c *cli.Context) error {
			return cmd.run(c.App.Writer, c.Args()...)
		},
		Flags: append(cmd.LintFlags(),
			cli.BoolFlag{
				Name:        "n",
				Usage:       "Hide golint missing comment warnings",
				Destination: &cmd.NoMissingComment,
			},
			cli.StringSliceFlag{
				Name:  "ignore-suffix",
				Usage: fmt.Sprintf("Filter out lint lines from files that have these suffixes (default: %s)", strings.Join(zblint.DefaultIgnoreSuffixes, ",")),
				Value: &cmd.IgnoreSuffixes,
			},
			cli.BoolFlag{
				Name:        "raw",
				Usage:       "match gometalinter output exactly, don't use logger",
				Destination: &cmd.Raw,
			},
		),
	}
}

func (cmd *cc) run(w io.Writer, args ...string) error {
	projects, err := project.Projects(&cmd.Context, args...)
	if err != nil {
		return err
	}

	// run go generate as necessary
	if _, err = projects.Build(project.TargetGenerate); err != nil {
		return err
	}

	if _, err = exec.LookPath("gometalinter"); err != nil {
		return err
	}

	pkgs, toRun, err := cmd.buildLists(projects)
	if err != nil {
		return err
	}

	return cmd.exec(w, pkgs, toRun)
}

func (cmd *cc) exec(w io.Writer, pkgs, toRun project.Packages) error {
	code := zbcontext.ExitOK

	for _, pkg := range pkgs {
		file, err := cmd.CacheFile(pkg)
		if err != nil {
			return err
		}

		if len(toRun) > 0 && toRun[0] == pkg {
			ecode, err := cmd.runLinter(w, pkg.Package.Dir, file)
			if err != nil {
				return err
			}
			if code == zbcontext.ExitOK {
				code = ecode
			}

			toRun = toRun[1:]
		} else {
			failed, err := cmd.ShowResult(w, file)
			if err != nil {
				return err
			}
			if code == zbcontext.ExitOK && failed {
				code = zbcontext.ExitFailed
			}
		}
	}

	if code != zbcontext.ExitOK {
		return cli.NewExitError("", code)
	}

	return nil
}

func (cmd *cc) buildLists(projects project.List) (pkgs, toRun project.Packages, err error) {
	for _, proj := range projects {
		for _, pkg := range proj.Packages {
			if pkg.IsVendored {
				continue
			}

			pkgs = append(pkgs, pkg)

			var foundResult bool
			if foundResult, err = cmd.HaveResult(pkg); err != nil {
				return
			}

			if !foundResult {
				toRun = append(toRun, pkg)
			}
		}
	}

	sort.Sort(&pkgs)
	sort.Sort(&toRun)

	return
}

func (cmd *cc) runLinter(w io.Writer, path, cacheFile string) (int, error) {
	code := zbcontext.ExitOK

	if err := os.MkdirAll(cmd.CacheDir, 0700); err != nil {
		return code, err
	}

	args := cmd.LintArgs()
	args = append(args, path)

	if !cmd.Raw {
		cmd.Logger.Debug(zbcontext.QuoteCommand("→ gometalinter", args))
	}

	pr, pw := io.Pipe()

	ecmd := exec.Command("gometalinter", args...) // nosec
	ecmd.Stdout = pw
	ecmd.Stderr = pw

	if err := ecmd.Start(); err != nil {
		return code, err
	}

	var group errgroup.Group
	group.Go(func() error {
		defer func() { _ = pw.Close() }() // nosec

		if ecmd == nil {
			return nil
		}

		ecode, err := zbcontext.ExitCode(ecmd.Wait())
		if err != nil {
			return err
		}

		if code == zbcontext.ExitOK {
			code = ecode
		}

		return nil
	})

	if err := cmd.ReadResult(w, pr, cacheFile); err != nil {
		return code, err
	}

	if err := group.Wait(); err != nil {
		return code, err
	}

	return code, nil
}