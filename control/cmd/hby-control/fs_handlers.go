package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var hiddenFileNames = map[string]struct{}{
	"authelia-ca.pem": {},
	"start-server.sh": {},
}

func (a *app) handleFSList(w http.ResponseWriter, r *http.Request) {
	full, display, err := a.resolveExisting(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(full)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if !info.IsDir() {
		if isHiddenDisplayPath(display) {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"path": display, "entries": []fileEntry{entryFor(display, info)}})
		return
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	files := make([]fileEntry, 0, len(entries))
	for _, ent := range entries {
		info, err := ent.Info()
		if err != nil {
			continue
		}
		p := path.Join(display, ent.Name())
		if display == "/" {
			p = "/" + ent.Name()
		}
		if isHiddenDisplayPath(p) {
			continue
		}
		files = append(files, entryFor(p, info))
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir
		}
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})
	writeJSON(w, http.StatusOK, map[string]any{"path": display, "entries": files})
}

func (a *app) handleFSRead(w http.ResponseWriter, r *http.Request) {
	full, display, err := a.resolveExisting(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(full)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if info.IsDir() {
		writeError(w, http.StatusBadRequest, "cannot read a directory")
		return
	}
	if info.Size() > a.cfg.MaxReadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file is too large for the editor; use download instead")
		return
	}
	data, err := os.ReadFile(full)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	encoding := "utf-8"
	content := string(data)
	if !utf8.Valid(data) {
		encoding = "base64"
		content = base64.StdEncoding.EncodeToString(data)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":     display,
		"name":     filepath.Base(full),
		"size":     info.Size(),
		"mode":     info.Mode().Perm().String(),
		"encoding": encoding,
		"content":  content,
	})
}

func (a *app) handleFSWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Path     string `json:"path"`
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	full, display, err := a.resolveForWrite(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	data := []byte(req.Content)
	if req.Encoding == "base64" {
		data, err = base64.StdEncoding.DecodeString(req.Content)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid base64 content")
			return
		}
	}
	mode := os.FileMode(0o640)
	if info, err := os.Stat(full); err == nil {
		if info.IsDir() {
			writeError(w, http.StatusBadRequest, "cannot write a directory")
			return
		}
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(full, data, mode); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": display, "size": len(data)})
}

func (a *app) handleFSDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Path  string   `json:"path"`
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	paths := req.Paths
	if req.Path != "" {
		paths = append(paths, req.Path)
	}
	if len(paths) == 0 {
		writeError(w, http.StatusBadRequest, "missing path")
		return
	}
	deleted := make([]string, 0, len(paths))
	for _, p := range paths {
		full, display, err := a.resolveExisting(p)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if display == "/" {
			writeError(w, http.StatusBadRequest, "refusing to delete the root directory")
			return
		}
		if err := os.RemoveAll(full); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		deleted = append(deleted, display)
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

func (a *app) handleFSMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Path        string   `json:"path"`
		Paths       []string `json:"paths"`
		Destination string   `json:"destination"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	paths := req.Paths
	if req.Path != "" {
		paths = append(paths, req.Path)
	}
	if len(paths) == 0 || req.Destination == "" {
		writeError(w, http.StatusBadRequest, "missing path or destination")
		return
	}
	destDir, destDisplay, err := a.resolveExisting(req.Destination)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	destInfo, err := os.Stat(destDir)
	if err != nil || !destInfo.IsDir() {
		writeError(w, http.StatusBadRequest, "destination must be a directory")
		return
	}

	moved := make([]map[string]string, 0, len(paths))
	for _, p := range paths {
		src, srcDisplay, err := a.resolveExisting(p)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if srcDisplay == "/" {
			writeError(w, http.StatusBadRequest, "refusing to move the root directory")
			return
		}
		targetDisplay := path.Join(destDisplay, path.Base(srcDisplay))
		if destDisplay == "/" {
			targetDisplay = "/" + path.Base(srcDisplay)
		}
		target, cleanTarget, err := a.resolveForWrite(targetDisplay)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if src == target {
			writeError(w, http.StatusBadRequest, "source and destination are the same")
			return
		}
		if strings.HasPrefix(filepath.Clean(target), filepath.Clean(src)+string(filepath.Separator)) {
			writeError(w, http.StatusBadRequest, "cannot move a directory into itself")
			return
		}
		if _, err := os.Lstat(target); err == nil {
			writeError(w, http.StatusConflict, "destination already exists: "+cleanTarget)
			return
		}
		if err := os.Rename(src, target); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		moved = append(moved, map[string]string{"from": srcDisplay, "to": cleanTarget})
	}
	writeJSON(w, http.StatusOK, map[string]any{"moved": moved})
}

func (a *app) handleFSArchive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Paths       []string `json:"paths"`
		Destination string   `json:"destination"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(req.Paths) == 0 || req.Destination == "" {
		writeError(w, http.StatusBadRequest, "missing paths or destination")
		return
	}
	dest, destDisplay, err := a.resolveForWrite(req.Destination)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := os.Lstat(dest); err == nil {
		writeError(w, http.StatusConflict, "archive already exists")
		return
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".hby-archive-*.tar")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	tw := tar.NewWriter(tmp)
	archived := 0
	for _, p := range req.Paths {
		full, display, err := a.resolveExisting(p)
		if err != nil {
			_ = tw.Close()
			_ = tmp.Close()
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if display == "/" {
			_ = tw.Close()
			_ = tmp.Close()
			writeError(w, http.StatusBadRequest, "refusing to archive the root directory")
			return
		}
		n, err := a.addPathToTar(tw, full, path.Base(display), tmpName)
		if err != nil {
			_ = tw.Close()
			_ = tmp.Close()
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		archived += n
	}
	if err := tw.Close(); err != nil {
		_ = tmp.Close()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tmp.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := os.Rename(tmpName, dest); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": destDisplay, "archived": archived})
}

func (a *app) handleFSExtract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Path        string `json:"path"`
		Destination string `json:"destination"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Path == "" || req.Destination == "" {
		writeError(w, http.StatusBadRequest, "missing path or destination")
		return
	}
	src, _, err := a.resolveExisting(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	destDir, destDisplay, err := a.resolveExisting(req.Destination)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	destInfo, err := os.Stat(destDir)
	if err != nil || !destInfo.IsDir() {
		writeError(w, http.StatusBadRequest, "destination must be a directory")
		return
	}
	f, err := os.Open(src)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer f.Close()

	var reader io.Reader = f
	if strings.HasSuffix(strings.ToLower(req.Path), ".tar.gz") || strings.HasSuffix(strings.ToLower(req.Path), ".tgz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid gzip archive")
			return
		}
		defer gz.Close()
		reader = gz
	}
	extracted, err := a.extractTar(reader, destDisplay)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"destination": destDisplay, "extracted": extracted})
}

func (a *app) handleFSMkdir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	full, display, err := a.resolveForWrite(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.MkdirAll(full, 0o750); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": display})
}

func (a *app) handleFSUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, a.cfg.MaxUploadBytes)
	if err := r.ParseMultipartForm(a.cfg.MaxUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid upload")
		return
	}
	dir, _, err := a.resolveExisting(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, "upload path must be a directory")
		return
	}
	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		writeError(w, http.StatusBadRequest, "missing upload file")
		return
	}
	uploaded := make([]string, 0, len(files))
	for _, fh := range files {
		if err := a.saveUploadedFile(dir, fh); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		uploaded = append(uploaded, fh.Filename)
	}
	writeJSON(w, http.StatusOK, map[string]any{"uploaded": uploaded})
}

func (a *app) saveUploadedFile(dir string, fh *multipart.FileHeader) error {
	name := filepath.Base(fh.Filename)
	if name == "." || name == string(filepath.Separator) {
		return errors.New("invalid file name")
	}
	if _, hidden := hiddenFileNames[name]; hidden {
		return errors.New("file is not accessible")
	}
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dstPath := filepath.Join(dir, name)
	if err := a.ensureWithinRootForExistingOrParent(dstPath); err != nil {
		return err
	}
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}

func (a *app) handleFSDownload(w http.ResponseWriter, r *http.Request) {
	full, _, err := a.resolveExisting(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(full)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if info.IsDir() {
		writeError(w, http.StatusBadRequest, "cannot download a directory")
		return
	}
	http.ServeFile(w, r, full)
}

func (a *app) addPathToTar(tw *tar.Writer, full, archiveName, skipFull string) (int, error) {
	archiveName = path.Clean(strings.ReplaceAll(archiveName, "\\", "/"))
	if archiveName == "." || archiveName == "/" || strings.HasPrefix(archiveName, "../") || archiveName == ".." {
		return 0, fmt.Errorf("invalid archive path %q", archiveName)
	}
	fullClean := filepath.Clean(full)
	skipClean := filepath.Clean(skipFull)
	written := 0
	err := filepath.WalkDir(full, func(current string, ent os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		currentClean := filepath.Clean(current)
		if currentClean == skipClean {
			if ent.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(fullClean, currentClean)
		if err != nil {
			return err
		}
		name := archiveName
		if rel != "." {
			name = path.Join(archiveName, filepath.ToSlash(rel))
		}
		if isHiddenDisplayPath("/" + name) {
			if ent.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := ent.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = name
		if info.IsDir() && !strings.HasSuffix(header.Name, "/") {
			header.Name += "/"
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			written++
			return nil
		}
		file, err := os.Open(currentClean)
		if err != nil {
			return err
		}
		if _, err := io.Copy(tw, file); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		written++
		return nil
	})
	return written, err
}

func (a *app) extractTar(reader io.Reader, destination string) (int, error) {
	tr := tar.NewReader(reader)
	extracted := 0
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return extracted, err
		}
		name, err := cleanTarEntryName(header.Name)
		if err != nil {
			return extracted, err
		}
		targetDisplay := path.Join(destination, name)
		if destination == "/" {
			targetDisplay = "/" + name
		}
		if isHiddenDisplayPath(targetDisplay) {
			return extracted, errors.New("archive contains an inaccessible file")
		}
		target, _, err := a.resolveForCreate(targetDisplay)
		if err != nil {
			return extracted, err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, modeOrDefault(header.FileInfo().Mode().Perm(), 0o750)); err != nil {
				return extracted, err
			}
		case tar.TypeReg, tar.TypeRegA:
			if _, err := os.Lstat(target); err == nil {
				return extracted, fmt.Errorf("refusing to overwrite existing file: %s", targetDisplay)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return extracted, err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, modeOrDefault(header.FileInfo().Mode().Perm(), 0o640))
			if err != nil {
				return extracted, err
			}
			if _, err := io.Copy(file, tr); err != nil {
				_ = file.Close()
				return extracted, err
			}
			if err := file.Close(); err != nil {
				return extracted, err
			}
		default:
			return extracted, fmt.Errorf("unsupported tar entry type for %s", header.Name)
		}
		extracted++
	}
	return extracted, nil
}

func cleanTarEntryName(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" || strings.HasPrefix(name, "/") {
		return "", errors.New("archive contains an invalid path")
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("archive contains a path traversal entry")
	}
	return clean, nil
}

func modeOrDefault(mode os.FileMode, fallback os.FileMode) os.FileMode {
	if mode == 0 {
		return fallback
	}
	return mode
}

type fileEntry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	IsDir   bool      `json:"isDir"`
	Size    int64     `json:"size"`
	Mode    string    `json:"mode"`
	ModTime time.Time `json:"modTime"`
}

func entryFor(display string, info os.FileInfo) fileEntry {
	return fileEntry{
		Name:    info.Name(),
		Path:    display,
		IsDir:   info.IsDir(),
		Size:    info.Size(),
		Mode:    info.Mode().Perm().String(),
		ModTime: info.ModTime(),
	}
}

func (a *app) resolveExisting(input string) (string, string, error) {
	full, display, err := a.resolveClean(input)
	if err != nil {
		return "", "", err
	}
	if err := a.ensureWithinRootExisting(full); err != nil {
		return "", "", err
	}
	if isHiddenDisplayPath(display) {
		return "", "", errors.New("file is not accessible")
	}
	return full, display, nil
}

func (a *app) resolveForWrite(input string) (string, string, error) {
	full, display, err := a.resolveClean(input)
	if err != nil {
		return "", "", err
	}
	if display == "/" {
		return "", "", errors.New("refusing to write the root directory")
	}
	if err := a.ensureWithinRootForExistingOrParent(full); err != nil {
		return "", "", err
	}
	if isHiddenDisplayPath(display) {
		return "", "", errors.New("file is not accessible")
	}
	return full, display, nil
}

func (a *app) resolveForCreate(input string) (string, string, error) {
	full, display, err := a.resolveClean(input)
	if err != nil {
		return "", "", err
	}
	if display == "/" {
		return "", "", errors.New("refusing to write the root directory")
	}
	if isHiddenDisplayPath(display) {
		return "", "", errors.New("file is not accessible")
	}
	if _, err := os.Lstat(full); err == nil {
		if err := a.ensureWithinRootExisting(full); err != nil {
			return "", "", err
		}
		return full, display, nil
	}
	parent := filepath.Dir(full)
	for {
		if _, err := os.Lstat(parent); err == nil {
			break
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", "", errors.New("could not find an existing parent directory")
		}
		parent = next
	}
	real, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", "", err
	}
	if err := a.ensureWithinRootReal(real); err != nil {
		return "", "", err
	}
	return full, display, nil
}

func isHiddenDisplayPath(display string) bool {
	if display == "/" {
		return false
	}
	for _, part := range strings.Split(strings.Trim(display, "/"), "/") {
		if _, hidden := hiddenFileNames[part]; hidden {
			return true
		}
	}
	return false
}

func (a *app) resolveClean(input string) (string, string, error) {
	input = strings.ReplaceAll(input, "\\", "/")
	clean := path.Clean("/" + strings.TrimPrefix(input, "/"))
	if clean == "." {
		clean = "/"
	}
	rel := strings.TrimPrefix(clean, "/")
	full := filepath.Clean(filepath.Join(a.cfg.Root, filepath.FromSlash(rel)))
	rootClean := filepath.Clean(a.cfg.Root)
	if full != rootClean {
		relToRoot, err := filepath.Rel(rootClean, full)
		if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) {
			return "", "", errors.New("path escapes configured root")
		}
	}
	return full, clean, nil
}

func (a *app) ensureWithinRootExisting(full string) error {
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return err
	}
	return a.ensureWithinRootReal(real)
}

func (a *app) ensureWithinRootForExistingOrParent(full string) error {
	if _, err := os.Lstat(full); err == nil {
		return a.ensureWithinRootExisting(full)
	}
	parent := filepath.Dir(full)
	real, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return err
	}
	return a.ensureWithinRootReal(real)
}

func (a *app) ensureWithinRootReal(real string) error {
	root := filepath.Clean(a.cfg.RootReal)
	real = filepath.Clean(real)
	if real == root {
		return nil
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("path escapes configured root")
	}
	return nil
}
