package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/pkg/sftp"
)

// demoFile is one entry in the invented filesystem the demo server serves. It is
// the only thing the SFTP browser and the fake editor ever see, which is the whole
// point: a recording made against it cannot leak anything real.
type demoFile struct {
	name    string // full POSIX path, e.g. /home/deploy/app/main.py
	dir     bool
	mode    os.FileMode
	content string
	mod     time.Time
}

// Name reports the base name, as os.FileInfo requires (the SFTP protocol sends
// base names in a listing and resolves them against the directory).
func (f *demoFile) Name() string { return path.Base(f.name) }
func (f *demoFile) Size() int64  { return int64(len(f.content)) }
func (f *demoFile) Mode() os.FileMode {
	if f.dir {
		return f.mode | os.ModeDir
	}
	return f.mode
}
func (f *demoFile) ModTime() time.Time { return f.mod }
func (f *demoFile) IsDir() bool        { return f.dir }
func (f *demoFile) Sys() any           { return nil }

// demoFS is the invented tree, keyed by full path.
type demoFS struct {
	files map[string]*demoFile
}

// homeDir is where the SFTP browser opens, and the user the fake shell prompts as.
const (
	demoUser = "deploy"
	demoHome = "/home/deploy"
	demoHost = "prod-web-1"
)

// newDemoFS builds the tree. Every byte of it is invented — the point of the demo
// server is that a recording made against it shows plausible content and no real
// content.
func newDemoFS() *demoFS {
	fs := &demoFS{files: map[string]*demoFile{}}

	// A fixed clock, so a re-recorded GIF is byte-identical rather than differing
	// only in the timestamps nobody can see anyway.
	base := time.Date(2026, 7, 21, 9, 14, 0, 0, time.UTC)
	at := func(days int) time.Time { return base.AddDate(0, 0, -days) }

	dirs := []struct {
		p    string
		days int
	}{
		{"/", 400}, {"/home", 400}, {demoHome, 1},
		{demoHome + "/app", 2}, {demoHome + "/app/static", 9},
		{demoHome + "/logs", 0}, {demoHome + "/backups", 4},
	}
	for _, d := range dirs {
		fs.files[d.p] = &demoFile{name: d.p, dir: true, mode: 0o755, mod: at(d.days)}
	}

	add := func(p string, days int, mode os.FileMode, content string) {
		fs.files[p] = &demoFile{name: p, mode: mode, content: content, mod: at(days)}
	}

	add(demoHome+"/docker-compose.yml", 3, 0o644, dockerCompose)
	add(demoHome+"/deploy.sh", 6, 0o755, deployScript)
	add(demoHome+"/.env.example", 12, 0o644, envExample)
	add(demoHome+"/README.md", 8, 0o644, readmeFile)
	add(demoHome+"/notes.txt", 1, 0o644, notesFile)
	add(demoHome+"/app/main.py", 2, 0o644, mainPy)
	add(demoHome+"/app/requirements.txt", 30, 0o644, "flask==3.0.3\ngunicorn==22.0.0\npsycopg[binary]==3.2.1\nredis==5.0.7\n")
	add(demoHome+"/app/static/style.css", 9, 0o644, ":root { --accent: #ff5fa2; }\n\nbody {\n  font: 16px/1.6 system-ui, sans-serif;\n  margin: 0 auto;\n  max-width: 48rem;\n}\n")
	add(demoHome+"/logs/access.log", 0, 0o644, accessLog)
	add(demoHome+"/logs/error.log", 0, 0o644, "2026-07-21 08:12:44 [warn] upstream timed out (110: Connection timed out)\n2026-07-21 08:41:02 [warn] client closed connection while waiting for request\n")
	add(demoHome+"/backups/db-2026-07-20.sql.gz", 1, 0o644, strings.Repeat("\x1f\x8b\x08demo-backup-payload", 900))

	return fs
}

// lookup resolves p, treating a relative path as relative to the demo home so the
// client's "." and "" both land somewhere sensible.
func (fs *demoFS) lookup(p string) (*demoFile, error) {
	f, ok := fs.files[fs.clean(p)]
	if !ok {
		return nil, os.ErrNotExist
	}
	return f, nil
}

func (fs *demoFS) clean(p string) string {
	if p == "" || p == "." {
		return demoHome
	}
	if !path.IsAbs(p) {
		p = path.Join(demoHome, p)
	}
	return path.Clean(p)
}

// readdir returns the direct children of dir, directories first then by name —
// the browser sorts too, but a server that answers in a stable order keeps a
// re-recorded GIF identical.
func (fs *demoFS) readdir(dir string) ([]os.FileInfo, error) {
	dir = fs.clean(dir)
	d, err := fs.lookup(dir)
	if err != nil {
		return nil, err
	}
	if !d.dir {
		return nil, errors.New("not a directory")
	}

	var out []os.FileInfo
	for p, f := range fs.files {
		if p != dir && path.Dir(p) == dir {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir() != out[j].IsDir() {
			return out[i].IsDir()
		}
		return out[i].Name() < out[j].Name()
	})
	return out, nil
}

// ---- sftp.Handlers ----

// listerat adapts a slice of FileInfo to sftp's paging ListerAt.
type listerat []os.FileInfo

func (l listerat) ListAt(dst []os.FileInfo, off int64) (int, error) {
	if off >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(dst, l[off:])
	if n+int(off) >= len(l) {
		return n, io.EOF
	}
	return n, nil
}

// Filelist answers List, Stat and Readlink.
func (fs *demoFS) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	switch r.Method {
	case "List":
		infos, err := fs.readdir(r.Filepath)
		if err != nil {
			return nil, err
		}
		return listerat(infos), nil
	case "Stat":
		f, err := fs.lookup(r.Filepath)
		if err != nil {
			return nil, err
		}
		return listerat{f}, nil
	}
	return nil, fmt.Errorf("demoserver: unsupported list method %q", r.Method)
}

// RealPath is what makes the browser open in /home/deploy: hop asks the server to
// resolve "." and starts wherever the answer points.
func (fs *demoFS) RealPath(p string) (string, error) { return fs.clean(p), nil }

// Fileread serves downloads and the local-open (`o`) copy.
func (fs *demoFS) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	f, err := fs.lookup(r.Filepath)
	if err != nil {
		return nil, err
	}
	if f.dir {
		return nil, errors.New("is a directory")
	}
	return strings.NewReader(f.content), nil
}

// Filewrite and Filecmd exist so an accidental upload or delete during a recording
// fails cleanly rather than panicking the server. The demo tree is read-only.
func (fs *demoFS) Filewrite(*sftp.Request) (io.WriterAt, error) {
	return nil, errors.New("demo filesystem is read-only")
}

func (fs *demoFS) Filecmd(*sftp.Request) error {
	return errors.New("demo filesystem is read-only")
}

// handlers wires the demo tree into an SFTP request server.
func (fs *demoFS) handlers() sftp.Handlers {
	return sftp.Handlers{FileGet: fs, FilePut: fs, FileCmd: fs, FileList: fs}
}
