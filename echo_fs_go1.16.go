//go:build go1.16
// +build go1.16

package echo

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type filesystem struct {
	// Filesystem is file system used by Static and File handlers to access files.
	// Defaults to os.DirFS(".")
	Filesystem fs.FS
}

func createFilesystem() filesystem {
	return filesystem{
		Filesystem: newDefaultFS(),
	}
}

// Static registers a new route with path prefix to serve static files from the provided root directory.
func (e *Echo) Static(pathPrefix, root string) *Route {
	subFs, err := subFS(e.Filesystem, root)
	if err != nil {
		// happens when `root` contains invalid path according to `fs.ValidPath` rules and we are unable to create FS
		panic(fmt.Errorf("invalid root given to echo.Static, err %w", err))
	}
	return e.Add(
		http.MethodGet,
		pathPrefix+"*",
		StaticDirectoryHandler(subFs, false),
	)
}

// StaticFS registers a new route with path prefix to serve static files from the provided file system.
func (e *Echo) StaticFS(pathPrefix string, fileSystem fs.FS) *Route {
	return e.Add(
		http.MethodGet,
		pathPrefix+"*",
		StaticDirectoryHandler(fileSystem, false),
	)
}

// StaticDirectoryHandler creates handler function to serve files from provided file system
// When disablePathUnescaping is set then file name from path is not unescaped and is served as is.
func StaticDirectoryHandler(fileSystem fs.FS, disablePathUnescaping bool) HandlerFunc {
	return func(c Context) error {
		p := c.Param("*")
		if !disablePathUnescaping { // when router is already unescaping we do not want to do is twice
			tmpPath, err := url.PathUnescape(p)
			if err != nil {
				return fmt.Errorf("failed to unescape path variable: %w", err)
			}
			p = tmpPath
		}

		// fs.FS.Open() already assumes that file names are relative to FS root path and considers name with prefix `/` as invalid
		name := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(p, "/")))
		fi, err := fs.Stat(fileSystem, name)
		if err != nil {
			return ErrNotFound
		}

		// If the request is for a directory and does not end with "/"
		p = c.Request().URL.Path // path must not be empty.
		if fi.IsDir() && len(p) > 0 && p[len(p)-1] != '/' {
			// Redirect to ends with "/"
			return c.Redirect(http.StatusMovedPermanently, p+"/")
		}
		return fsFile(c, name, fileSystem)
	}
}

// FileFS registers a new route with path to serve file from the provided file system.
func (e *Echo) FileFS(path, file string, filesystem fs.FS, m ...MiddlewareFunc) *Route {
	return e.GET(path, StaticFileHandler(file, filesystem), m...)
}

// StaticFileHandler creates handler function to serve file from provided file system
func StaticFileHandler(file string, filesystem fs.FS) HandlerFunc {
	return func(c Context) error {
		return fsFile(c, file, filesystem)
	}
}

// defaultFS emulates os.Open behaviour with filesystem opened by `os.DirFs`. Difference between `os.Open` and `fs.Open`
// is that FS does not allow to open path that start with `..` or `/` etc. For example previously you could have `../images`
// in your application but `fs := os.DirFS("./")` would not allow you to use `fs.Open("../images")` and this would break
// all old applications that rely on being able to traverse up from current executable run path.
// NB: private because you really should use fs.FS implementation instances
type defaultFS struct {
	prefix string
	fs     fs.FS
}

func newDefaultFS() *defaultFS {
	dir, _ := os.Getwd()
	return &defaultFS{
		prefix: dir,
		fs:     os.DirFS(dir),
	}
}

func (fs defaultFS) Open(name string) (fs.File, error) {
	return fs.fs.Open(name)
}

func subFS(currentFs fs.FS, root string) (fs.FS, error) {
	root = filepath.ToSlash(filepath.Clean(root)) // note: fs.FS operates only with slashes. `ToSlash` is necessary for Windows
	if dFS, ok := currentFs.(*defaultFS); ok {
		// we need to make exception for `defaultFS` instances as it interprets root prefix differently from fs.FS to
		// allow cases when root is given as `../somepath` which is not valid for fs.FS
		root = filepath.Join(dFS.prefix, root)
		return &defaultFS{
			prefix: root,
			fs:     os.DirFS(root),
		}, nil
	}
	return fs.Sub(currentFs, root)
}