package bridge

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// homeDir returns the current user's home directory.
func homeDir() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return h, nil
}

// dirExists reports whether path is a directory (true) or a symlink to a
// directory (also true). Missing or any other error returns false.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// isSymlink reports whether path is a symlink (regardless of where it points).
func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSymlink != 0
}

// isEmptyDir reports true for a directory with no entries (or one that does
// not exist). Used before deciding whether a migrate step is even needed.
func isEmptyDir(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return true
	}
	return len(entries) == 0
}

// copyTree recursively copies src into dst, creating parents as needed. If a
// destination file already exists with identical content it is left alone;
// differing content returns an error (the caller is expected to surface it
// rather than silently overwriting).
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case info.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode().IsRegular():
			return copyFile(path, target, info.Mode().Perm())
		default:
			// Skip devices, sockets, etc. Symlinks inside a memory dir
			// are not expected; if we ever support them we'd handle here.
			return nil
		}
	})
}

// copyFile copies a single file. If dst exists with identical bytes, no-op;
// if it exists with different bytes, returns an error.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if existing, err := os.Open(dst); err == nil {
		same, err := streamsEqual(existing, in)
		existing.Close()
		if err != nil {
			return err
		}
		if same {
			return nil
		}
		return fmt.Errorf("destination %s exists with different content", dst)
	}

	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// streamsEqual reads both readers to EOF and reports whether they're
// byte-identical.
func streamsEqual(a, b io.Reader) (bool, error) {
	const bufSize = 32 * 1024
	bufA := make([]byte, bufSize)
	bufB := make([]byte, bufSize)
	for {
		nA, errA := io.ReadFull(a, bufA)
		nB, errB := io.ReadFull(b, bufB)
		if nA != nB {
			return false, nil
		}
		if string(bufA[:nA]) != string(bufB[:nB]) {
			return false, nil
		}
		if errA == io.EOF || errA == io.ErrUnexpectedEOF {
			if errB == io.EOF || errB == io.ErrUnexpectedEOF {
				return true, nil
			}
			return false, nil
		}
		if errA != nil {
			return false, errA
		}
		if errB != nil {
			return false, errB
		}
	}
}

// removeIfEmpty rm's path if it's an empty directory. Useful when migrating
// content out — we don't want a stale empty dir confusing later bridges.
// Missing path: silent no-op.
func removeIfEmpty(path string) error {
	if !dirExists(path) {
		return nil
	}
	if !isEmptyDir(path) {
		return nil
	}
	return os.Remove(path)
}

// listDirNames returns the names of immediate subdirectories of dir, sorted.
// Missing dir returns nil, no error. Used by --list-scopes paths to enumerate
// per-instance harnesses (Claude Code projects, OpenClaw workspaces, etc.).
func listDirNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}
