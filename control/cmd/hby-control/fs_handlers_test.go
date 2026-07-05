package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHiddenFilesAreNotExposedByFileAPI(t *testing.T) {
	app, root := newTestFileApp(t)
	mustWriteFile(t, filepath.Join(root, "authelia-ca.pem"), "certificate")
	mustWriteFile(t, filepath.Join(root, "start-server.sh"), "#!/bin/sh\n")
	mustWriteFile(t, filepath.Join(root, "server.properties"), "motd=test\n")

	listReq := httptest.NewRequest(http.MethodGet, "/api/fs/list?path=/", nil)
	listRes := httptest.NewRecorder()
	app.handleFSList(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRes.Code, listRes.Body.String())
	}

	var list struct {
		Entries []fileEntry `json:"entries"`
	}
	if err := json.NewDecoder(listRes.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, entry := range list.Entries {
		names[entry.Name] = true
	}
	for _, name := range []string{"authelia-ca.pem", "start-server.sh"} {
		if names[name] {
			t.Fatalf("hidden file %q was listed", name)
		}
	}
	if !names["server.properties"] {
		t.Fatal("expected visible file server.properties to be listed")
	}

	assertFileRequestRejected(t, app.handleFSRead, http.MethodGet, "/api/fs/read?path=/authelia-ca.pem", nil)
	assertFileRequestRejected(t, app.handleFSDownload, http.MethodGet, "/api/fs/download?path=/start-server.sh", nil)
	assertFileRequestRejected(t, app.handleFSWrite, http.MethodPost, "/api/fs/write", []byte(`{"path":"/authelia-ca.pem","content":"new","encoding":"utf-8"}`))
	assertFileRequestRejected(t, app.handleFSDelete, http.MethodPost, "/api/fs/delete", []byte(`{"path":"/start-server.sh"}`))
}

func TestBulkMoveArchiveAndExtract(t *testing.T) {
	app, root := newTestFileApp(t)
	mustWriteFile(t, filepath.Join(root, "alpha.txt"), "alpha")
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o750); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(root, "dir", "beta.txt"), "beta")
	if err := os.Mkdir(filepath.Join(root, "moved"), 0o750); err != nil {
		t.Fatal(err)
	}

	assertJSONOK(t, app.handleFSMove, "/api/fs/move", map[string]any{
		"paths":       []string{"/alpha.txt"},
		"destination": "/moved",
	})
	if _, err := os.Stat(filepath.Join(root, "moved", "alpha.txt")); err != nil {
		t.Fatal(err)
	}

	assertJSONOK(t, app.handleFSArchive, "/api/fs/archive", map[string]any{
		"paths":       []string{"/moved/alpha.txt", "/dir"},
		"destination": "/bundle.tar",
	})
	if _, err := os.Stat(filepath.Join(root, "bundle.tar")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "out"), 0o750); err != nil {
		t.Fatal(err)
	}
	assertJSONOK(t, app.handleFSExtract, "/api/fs/extract", map[string]any{
		"path":        "/bundle.tar",
		"destination": "/out",
	})
	assertFileContent(t, filepath.Join(root, "out", "alpha.txt"), "alpha")
	assertFileContent(t, filepath.Join(root, "out", "dir", "beta.txt"), "beta")
}

func TestExtractRejectsTarPathTraversal(t *testing.T) {
	app, root := newTestFileApp(t)
	if err := os.Mkdir(filepath.Join(root, "out"), 0o750); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "../evil.txt", Mode: 0o640, Size: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("evil")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "evil.tar"), buf.Bytes(), 0o640); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"path": "/evil.tar", "destination": "/out"})
	if err != nil {
		t.Fatal(err)
	}
	assertFileRequestRejected(t, app.handleFSExtract, http.MethodPost, "/api/fs/extract", body)
}

func TestExtractCreatesMissingParentDirectories(t *testing.T) {
	app, root := newTestFileApp(t)
	if err := os.Mkdir(filepath.Join(root, "out"), 0o750); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "nested/file.txt", Mode: 0o640, Size: 7}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("content")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested.tar"), buf.Bytes(), 0o640); err != nil {
		t.Fatal(err)
	}
	assertJSONOK(t, app.handleFSExtract, "/api/fs/extract", map[string]any{
		"path":        "/nested.tar",
		"destination": "/out",
	})
	assertFileContent(t, filepath.Join(root, "out", "nested", "file.txt"), "content")
}

func newTestFileApp(t *testing.T) (*app, string) {
	t.Helper()
	root := t.TempDir()
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config{
		Root:           root,
		RootReal:       rootReal,
		MaxReadBytes:   1024 * 1024,
		MaxUploadBytes: 1024 * 1024,
		StopTimeout:    time.Second,
		LogBytes:       1024,
	}
	return newApp(cfg, newSupervisor(nil, root, time.Second, 1024)), root
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
}

func assertFileContent(t *testing.T, path string, expected string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != expected {
		t.Fatalf("%s content = %q, want %q", path, string(data), expected)
	}
}

func assertJSONOK(t *testing.T, handler http.HandlerFunc, target string, payload any) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	res := httptest.NewRecorder()
	handler(res, req)
	if res.Code < 200 || res.Code >= 300 {
		t.Fatalf("POST %s failed with %d: %s", target, res.Code, res.Body.String())
	}
}

func assertFileRequestRejected(t *testing.T, handler http.HandlerFunc, method, target string, body []byte) {
	t.Helper()
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	res := httptest.NewRecorder()
	handler(res, req)
	if res.Code >= 200 && res.Code < 300 {
		t.Fatalf("%s %s unexpectedly succeeded with %d: %s", method, target, res.Code, res.Body.String())
	}
}
