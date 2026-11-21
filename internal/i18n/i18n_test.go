package i18n

import "testing"

// 语言状态本身的行为。文案表的检查在 messages_test.go。
func TestLanguageState(t *testing.T) {
	t.Cleanup(func() { SetLanguage("zh-Hans") })

	SetLanguage("zh-Hans")
	if got := Language(); got != "zh-Hans" {
		t.Errorf("Language() = %q", got)
	}
	if English() {
		t.Error("中文下 English() 应为 false")
	}

	// "en-GB" 也要算英文：只认前缀，不认整串。
	SetLanguage("en-GB")
	if !English() {
		t.Error("en-GB 之后 English() 应为 true")
	}

	// 不认识的语言回到中文：界面原文就是中文。
	SetLanguage("fr")
	if got := Language(); got != "zh-Hans" {
		t.Errorf("法语之后 Language() = %q", got)
	}

	for _, c := range []struct {
		preferred []string
		want      string
	}{
		{[]string{"fr", "en-US"}, "en"},
		{[]string{"fr", "zh-CN"}, "zh-Hans"},
		{nil, "en"},
	} {
		if got := Pick(c.preferred); got != c.want {
			t.Errorf("Pick(%v) = %q，想要 %q", c.preferred, got, c.want)
		}
	}
}
