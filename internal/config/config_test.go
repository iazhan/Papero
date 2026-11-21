package config

import "testing"

// 老配置里没有 language 字段，读到的是空串；空串必须等于「中文」，
// 不能因为空值就落到英文去。
func TestLanguageNormalize(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string
	}{
		{"", ""},
		{"zh-Hans", ""},
		{"zh-CN", ""},
		{"en", "en"},
		{"en-US", "en"},
		{"EN-GB", ""}, // 大小写不一致就当没设过，规范化在写之前做
		{"fr", ""},
	} {
		s := Default()
		s.Language = c.in
		s.normalize()
		if s.Language != c.want {
			t.Errorf("language=%q 规范化后 = %q，想要 %q", c.in, s.Language, c.want)
		}
	}
}

// 空配置默认就是中文：界面原文是中文，英文得用户自己选。
func TestDefaultIsChinese(t *testing.T) {
	if got := Default().Language; got != "" {
		t.Errorf("默认语言 = %q，想要空串（中文）", got)
	}
}

// 旧配置只有一个库的记忆（LastOpen/Active），要搬进 Sessions 再清空，
// 否则「按库记标签页」永远从零开始，每次切库都回到第一篇。
func TestSessionsMigrate(t *testing.T) {
	s := Default()
	s.Vault = `D:/Note/Note`
	s.LastOpen = []string{"a.md", "b.png"}
	s.Active = "b.png"
	s.normalize()

	got := s.Session(s.Vault)
	if len(got.LastOpen) != 2 || got.LastOpen[0] != "a.md" || got.Active != "b.png" {
		t.Errorf("旧字段没搬进 Sessions：%+v", got)
	}
	if len(s.LastOpen) != 0 || s.Active != "" {
		t.Error("旧字段应当清空，免得两处各说一套")
	}

	// 再跑一次不能把已有的记忆盖掉
	s.SetSession(s.Vault, Session{LastOpen: []string{"c.md"}})
	s.LastOpen = []string{"d.md"}
	s.normalize()
	if got := s.Session(s.Vault).LastOpen; len(got) != 1 || got[0] != "c.md" {
		t.Errorf("已有记忆被旧字段覆盖：%v", got)
	}
}

// 同一个库的两种路径写法要落到同一个键上，否则会变成两套记忆。
func TestSessionKeyNormalized(t *testing.T) {
	a := SessionKey(`D:/Note/Note`)
	b := SessionKey(`D:/Note/Note/`)
	if a != b {
		t.Errorf("路径写法不同就分家了：%q vs %q", a, b)
	}
}

// 工作区间列表按归一后的路径去重：同一个库的两种写法只留一条。
func TestVaultsDedupe(t *testing.T) {
	s := Default()
	s.Vaults = []string{`D:/Note/Note`, `D:/Note/Note/`, `C:/Other`}
	s.normalize()
	if len(s.Vaults) != 2 {
		t.Errorf("去重后 = %v，想要 2 条", s.Vaults)
	}
	if s.Vaults[0] != `D:/Note/Note` || s.Vaults[1] != `C:/Other` {
		t.Errorf("保留的顺序或原样被改了：%v", s.Vaults)
	}
}
