package plugin

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func validManifest() *Manifest {
	return &Manifest{
		ID:          "dev.example.hello",
		Name:        "Hello",
		Version:     "1.0.0",
		Runtime:     "process",
		Main:        "hello.exe",
		Permissions: []string{"vault:read", "document:write"},
		Contributes: Contributes{
			Commands: []Command{{ID: "hello.run", Title: "运行 Hello", When: "document"}},
		},
	}
}

func TestValidate(t *testing.T) {
	if err := validManifest().Validate(); err != nil {
		t.Fatalf("合法清单被拒绝：%v", err)
	}

	cases := map[string]func(*Manifest){
		"大写 id":       func(m *Manifest) { m.ID = "Dev.Example" },
		"带斜杠的 id":     func(m *Manifest) { m.ID = "dev/example" },
		"缺少名字":        func(m *Manifest) { m.Name = "" },
		"未知运行方式":      func(m *Manifest) { m.Runtime = "native" },
		"进程插件没入口":     func(m *Manifest) { m.Main = "" },
		"未知权限":        func(m *Manifest) { m.Permissions = []string{"root"} },
		"命令缺少标题":      func(m *Manifest) { m.Contributes.Commands[0].Title = "" },
		"命令的 when 非法": func(m *Manifest) { m.Contributes.Commands[0].When = "always" },
	}
	for name, mutate := range cases {
		m := validManifest()
		mutate(m)
		if err := m.Validate(); err == nil {
			t.Errorf("%s：应当被拒绝", name)
		}
	}
}

// buildZip 打一个插件包。
func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInstallAndUninstall(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	manifest := validManifest()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	pkg := buildZip(t, map[string]string{
		"papero-plugin.json": string(data),
		"hello.exe":          "not really an executable",
	})

	sum := sha256.Sum256(pkg)
	got, err := m.Install(pkg, InstallOptions{SHA256: hex.EncodeToString(sum[:])})
	if err != nil {
		t.Fatalf("安装失败：%v", err)
	}
	if got.ID != manifest.ID {
		t.Errorf("安装的插件 id = %q", got.ID)
	}
	if _, err := os.Stat(filepath.Join(dir, manifest.ID, "hello.exe")); err != nil {
		t.Errorf("插件文件没有解压出来：%v", err)
	}

	list, err := m.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("插件数 = %d，想要 1", len(list))
	}
	if m.Find(manifest.ID) == nil {
		t.Error("Load 之后找不到插件")
	}

	if err := m.Uninstall(manifest.ID); err != nil {
		t.Fatalf("卸载失败：%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, manifest.ID)); !os.IsNotExist(err) {
		t.Error("卸载后目录仍在")
	}
}

func TestInstallRejectsUnsafePaths(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	pkg := buildZip(t, map[string]string{
		"papero-plugin.json": "{}",
		"../../evil.txt":     "x",
	})
	if _, err := m.Install(pkg, InstallOptions{}); err == nil {
		t.Fatal("包含越界路径的包应当被拒绝")
	}
}

func TestInstallChecksChecksum(t *testing.T) {
	m := NewManager(t.TempDir())
	pkg := buildZip(t, map[string]string{"papero-plugin.json": "{}"})
	if _, err := m.Install(pkg, InstallOptions{SHA256: "deadbeef"}); err == nil {
		t.Fatal("摘要不符时应当拒绝安装")
	}
}

func TestHostFunc(t *testing.T) {
	var h Host = HostFunc(func(req Request) (Response, error) {
		return Response{Markdown: "收到：" + req.Document.Path}, nil
	})
	resp, err := h.Invoke(Request{Document: Document{Path: "a.md"}})
	if err != nil || resp.Markdown != "收到：a.md" {
		t.Fatalf("Invoke = %+v, %v", resp, err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
}
