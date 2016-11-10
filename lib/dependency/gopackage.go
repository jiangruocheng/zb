package dependency

import (
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/src-d/go-git.v4/core"
	"jrubin.io/zb/lib/zbcontext"
)

type GoPackage struct {
	*build.Package
	*zbcontext.Context
	Path              string
	GitCommit         core.Hash
	ProjectImportPath string

	dependencies []Dependency
}

var _ Dependency = (*GoPackage)(nil)

func (pkg GoPackage) Name() string {
	return pkg.Path
}

func (pkg GoPackage) Build() error {
	if !pkg.IsCommand() {
		return pkg.Install()
	}

	args := []string{"build"}
	args = append(args, pkg.BuildArgs(pkg.Package, &pkg.GitCommit)...)
	args = append(args, "-o", pkg.Name())
	args = append(args, pkg.ImportPath)

	if err := pkg.GoExec(args...); err != nil {
		return err
	}

	return pkg.Touch(pkg.Name())
}

func (pkg GoPackage) Install() error {
	args := []string{"install"}
	args = append(args, pkg.BuildArgs(pkg.Package, &pkg.GitCommit)...)
	args = append(args, pkg.ImportPath)

	if err := pkg.GoExec(args...); err != nil {
		return err
	}

	return pkg.Touch(zbcontext.InstallPath(pkg.Package))
}

func (pkg GoPackage) ModTime() time.Time {
	i, err := os.Stat(pkg.Path)
	if err != nil {
		return time.Time{}
	}

	return i.ModTime()
}

func (pkg GoPackage) files() []Dependency {
	var files []string

	files = append(files, pkg.GoFiles...)
	files = append(files, pkg.CgoFiles...)
	files = append(files, pkg.CFiles...)
	files = append(files, pkg.CXXFiles...)
	files = append(files, pkg.MFiles...)
	files = append(files, pkg.HFiles...)
	files = append(files, pkg.FFiles...)
	files = append(files, pkg.SFiles...)
	files = append(files, pkg.SwigFiles...)
	files = append(files, pkg.SwigCXXFiles...)
	files = append(files, pkg.SysoFiles...)
	// files = append(files, pkg.TestGoFiles...)
	// files = append(files, pkg.XTestGoFiles...)

	gofiles := make([]Dependency, len(files))
	for i, f := range files {
		gofiles[i] = NewGoFile(pkg.Context, pkg.ProjectImportPath, filepath.Join(pkg.Dir, f))
	}

	return gofiles
}

func (pkg GoPackage) packages() ([]Dependency, error) {
	var pkgs []Dependency

	imports := pkg.Imports

	for _, i := range imports {
		if i == "C" {
			continue
		}

		p, err := pkg.Import(i, pkg.Dir)
		if err != nil {
			return nil, err
		}

		base := pkg.Context.ImportPathToDir(pkg.ProjectImportPath)
		if !strings.HasPrefix(p.Dir, base) {
			continue
		}

		pkgs = append(pkgs, &GoPackage{
			ProjectImportPath: pkg.ProjectImportPath,
			Path:              p.PkgObj,
			Package:           p,
			Context:           pkg.Context,
		})
	}

	return pkgs, nil
}

func (pkg *GoPackage) Buildable() bool {
	return true
}

func (pkg *GoPackage) Dependencies() ([]Dependency, error) {
	if pkg.dependencies != nil {
		return pkg.dependencies, nil
	}

	pkgs, err := pkg.packages()
	if err != nil {
		return nil, err
	}

	pkg.dependencies = pkgs
	pkg.dependencies = append(pkg.dependencies, pkg.files()...)

	return pkg.dependencies, nil
}
