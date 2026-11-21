package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"papero/internal/i18n"
	"papero/internal/update"
)

// noteLines 用的正则：更新日志里的 markdown 只在浮层里摊平，不渲染。
var (
	noteLink   = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	noteMark   = regexp.MustCompile("\\*\\*|__|`")
	noteBullet = regexp.MustCompile(`^\s*([-*+]|\d+[.)])\s+`)
	noteBare   = regexp.MustCompile(`https?://\S+`)
)

// 自更新：问 GitHub Releases 有没有新版本，有就下载、校验、换掉正在运行的 exe，
// 然后重启。和 mygo 自带的 Updater 相比这里不签名——本仓库的流水线出的是裸 exe
// 加一份 SHA256SUMS.txt，所以校验停在「摘要对得上」。将来换成签名发布，只需要
// 换掉 updater 的实现，下面这套状态机不用动。
//
// 能不能换掉自己取决于 exe 所在目录可不可写：mygo 的安装包把它装在
// %LOCALAPPDATA%\Programs\Papero，就是为了这一点；绿色版放在只读目录时，
// 安装那一步会失败并说清原因。

// 检查节奏：启动 20 秒后查一次（别和启动抢磁盘和网络），之后每 6 小时。
const (
	firstUpdateCheck = 20 * time.Second
	updateEvery      = 6 * time.Hour
)

// 两种超时分开：问一句很快，下载 24MB 在慢网上要几分钟。
const (
	checkTimeout    = 30 * time.Second
	downloadTimeout = 10 * time.Minute
)

// updater 是更新器的四个动作。githubUpdater 是真的那个，测试里换成假的。
type updater interface {
	// Check 返回比当前新的版本，没有更新时返回 nil。
	Check(ctx context.Context) (*update.Release, error)
	// Fetch 下载这个平台的包，校验完返回落在磁盘上的路径。
	Fetch(ctx context.Context, rel *update.Release, progress func(done, total int64)) (string, error)
	// Install 把下好的文件换到正在运行的 exe 的位置。
	Install(path string) error
	// Notes 取 after 到 upto 之间每个版本的更新日志，从新到旧，外加总条数。
	Notes(ctx context.Context, after, upto string) ([]update.Note, int, error)
}

// githubUpdater 是 updater 的真实实现：GitHub 官方 release。
type githubUpdater struct{ client *http.Client }

func (u githubUpdater) Check(ctx context.Context) (*update.Release, error) {
	return update.Check(ctx, u.client, update.Repo, appVersion)
}

func (u githubUpdater) Fetch(ctx context.Context, rel *update.Release, progress func(done, total int64)) (string, error) {
	exe, err := update.Executable()
	if err != nil {
		return "", err
	}
	sums, err := update.FetchSums(ctx, u.client, rel.Sums)
	if err != nil {
		return "", err
	}
	return update.Fetch(ctx, u.client, rel, filepath.Dir(exe), sums, progress)
}

func (u githubUpdater) Install(path string) error {
	exe, err := update.Executable()
	if err != nil {
		return err
	}
	return update.Install(path, exe)
}

func (u githubUpdater) Notes(ctx context.Context, after, upto string) ([]update.Note, int, error) {
	return update.Notes(ctx, u.client, update.Repo, after, upto)
}

// updateState 是自更新这一块的状态，全部由下面几个方法改。
type updateState struct {
	svc updater
	// checking / downloading 是正在进行的事，同一时间只允许一件。
	checking, downloading bool
	// rel 是查到的新版本；path 是下好的文件；ready 是它的版本号，等重启。
	rel   *update.Release
	path  string
	ready string
	// got / total 是下载进度，字节。
	got, total int64
	// status 是一句话状态，显示在关于页那一行的说明位。
	status string
	// notes 是从当前版本之后到目标版本之间每个版本的日志；total 是全部条数
	// （超过上限时 notes 只留最近的）。loading / err 是取它们的过程。
	notes        []update.Note
	notesTotal   int
	notesLoading bool
	notesErr     string
	// notesAfter 是这批日志的起点（不含），副标题要用它算「从哪一版到哪一版」
	notesAfter string
	// overlay 是更新日志浮层开着。
	overlay bool
	// updatedTo 是这次启动是从旧版本升上来的那个版本号，空 = 不是升级。
	updatedTo string
	// next 是下次自动检查的时刻；blockedUntil 是限速期间不许再查的时刻。
	next         time.Time
	blockedUntil time.Time
	// relaunch 重启应用；测试换掉它，免得测试里真的把进程重启。
	relaunch func()
}

// updateTick 每帧被调用：到点了就在后台问一次。
func (a *App) updateTick(c *ui.Context) {
	u := &a.upd
	now := c.Now()
	if u.next.IsZero() {
		u.next = now.Add(firstUpdateCheck)
	}
	if blocked := u.blockedUntil; !blocked.IsZero() && now.Before(blocked) {
		// 限速还没解除：下一次排到解除之后，中间一帧都不查
		u.next = blocked
	} else if !now.Before(u.next) {
		u.next = now.Add(updateEvery)
		if !a.cfg.NoUpdateCheck {
			a.checkUpdate(false)
		}
	}
	// 睡到下一次该检查的时刻：闲着的时候一帧都不用画。
	c.After(u.next.Sub(now))
}

// updateService 返回更新器，第一次用时装配。
func (a *App) updateService() updater {
	if a.upd.svc == nil {
		a.upd.svc = githubUpdater{client: &http.Client{}}
	}
	return a.upd.svc
}

// checkUpdate 问一次有没有新版本。manual 是用户自己按的按钮。
func (a *App) checkUpdate(manual bool) {
	u := &a.upd
	if u.checking || u.downloading {
		return
	}
	if u.ready != "" {
		if manual {
			u.status = fmt.Sprintf(i18n.T("settings.update.ready"), "v"+u.ready)
		}
		return
	}
	if u.blockedUntil.After(time.Now()) {
		// 点也没用：先把原因说清楚，别又撞一次墙
		if manual {
			u.status = (&update.RateLimitError{Reset: u.blockedUntil}).Error()
		}
		return
	}
	u.checking = true
	if manual || u.status == "" {
		u.status = i18n.T("settings.update.checking")
	}
	svc := a.updateService()
	work := func() (*update.Release, error) {
		ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
		defer cancel()
		return svc.Check(ctx)
	}
	// 没有真窗口（测试、-shot）时同步跑完，否则结果永远回不来
	if a.win == nil {
		rel, err := work()
		a.checked(rel, err)
		return
	}
	go func() {
		rel, err := work()
		a.win.Update(func() { a.checked(rel, err) })
	}()
}

// checked 收下一次检查的结果。
func (a *App) checked(rel *update.Release, err error) {
	u := &a.upd
	u.checking = false
	switch {
	case err != nil:
		// 「还没发过版本」是正常状态，不是错误：别把 404 当失败吓人
		if errors.Is(err, update.ErrNoReleases) {
			u.status = i18n.T("settings.update.none")
			u.rel = nil
			return
		}
		var rl *update.RateLimitError
		if errors.As(err, &rl) {
			// 限速不是「更新失败」，是一段时间内查不了：说清多久之后能再查
			u.status = rl.Error()
			u.blockedUntil = rl.Reset
			return
		}
		u.status = fmt.Sprintf(i18n.T("settings.update.failed"), err.Error())
	case rel == nil:
		u.rel = nil
		u.status = i18n.T("settings.update.latest")
	default:
		u.rel = rel
		u.status = fmt.Sprintf(i18n.T("settings.update.found"), "v"+rel.Version)
		// 用户点开更新日志之前就把它们取好：落后十几个版本时这一趟要一两秒
		a.loadNotes(appVersion, rel.Version)
	}
}

// downloadUpdate 下载查到的那一版。
func (a *App) downloadUpdate() {
	u := &a.upd
	if u.rel == nil || u.downloading || u.ready != "" {
		return
	}
	rel := u.rel
	u.downloading = true
	u.got, u.total = 0, rel.File.Size
	svc := a.updateService()
	report := func(done, total int64) {
		if a.win == nil {
			u.got, u.total = done, total
			return
		}
		a.win.Update(func() { u.got, u.total = done, total })
	}
	work := func() (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
		defer cancel()
		return svc.Fetch(ctx, rel, report)
	}
	if a.win == nil {
		path, err := work()
		u.fetched(rel, path, err)
		return
	}
	go func() {
		path, err := work()
		a.win.Update(func() { u.fetched(rel, path, err) })
	}()
}

// fetched 收下一次下载的结果。
func (u *updateState) fetched(rel *update.Release, path string, err error) {
	u.downloading = false
	if err != nil {
		u.status = fmt.Sprintf(i18n.T("settings.update.failed"), err.Error())
		return
	}
	u.path = path
	u.ready = rel.Version
	u.status = fmt.Sprintf(i18n.T("settings.update.ready"), "v"+rel.Version)
}

// installUpdate 换掉 exe 并重启。没下载过就先下载。
func (a *App) installUpdate() {
	u := &a.upd
	if u.ready == "" {
		a.downloadUpdate()
		return
	}
	// 先存盘：换完就重启，未保存的改动不能靠「关窗口时再存」
	a.saveAll()
	if err := a.updateService().Install(u.path); err != nil {
		u.status = fmt.Sprintf(i18n.T("settings.update.failed"), err.Error())
		return
	}
	if u.relaunch != nil {
		u.relaunch()
		return
	}
	mygo.App.Relaunch()
}

// updateButton 是那一行的按钮：按当前状态决定说什么、点什么。
func (a *App) updateButton(c *ui.Context) {
	u := &a.upd
	switch {
	case u.downloading:
		// 说明位已经在报百分比了，按钮位置空着
		return
	case u.ready != "":
		if ui.Button(c, i18n.T("settings.update.restart")).Clicked() {
			a.installUpdate()
		}
	case u.rel != nil:
		label := fmt.Sprintf(i18n.T("settings.update.download_version"), "v"+u.rel.Version)
		if ui.Button(c, label).Clicked() {
			a.downloadUpdate()
		}
	default:
		if ui.Button(c, i18n.T("settings.update.check")).Clicked() {
			a.checkUpdate(true)
		}
	}
}

// updateHint 是那一行左边的说明：正在下载时百分比走这里，其余时候是状态。
func (a *App) updateHint() string {
	u := &a.upd
	if u.downloading {
		pct := 0
		if u.total > 0 {
			pct = int(u.got * 100 / u.total)
		}
		return fmt.Sprintf(i18n.T("settings.update.progress"), pct)
	}
	if u.status == "" {
		// 还没查过：先说清更新从哪来
		return i18n.T("settings.update.source")
	}
	return u.status
}

// cleanupUpdate 删掉上次更新让位出来的旧 exe。
//
// 更新时间那会删不掉——进程还开着它。只能在启动时清。
func cleanupUpdate() {
	if exe, err := update.Executable(); err == nil {
		update.Cleanup(exe)
	}
}

// loadNotes 取 (after, upto] 之间每个版本的更新日志。
func (a *App) loadNotes(after, upto string) {
	u := &a.upd
	u.notesLoading = true
	u.notesErr = ""
	u.notesAfter = after
	svc := a.updateService()
	work := func() ([]update.Note, int, error) {
		ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
		defer cancel()
		return svc.Notes(ctx, after, upto)
	}
	// 没有真窗口时同步跑完，否则结果回不来
	if a.win == nil {
		notes, total, err := work()
		u.notesLoaded(notes, total, err)
		return
	}
	go func() {
		notes, total, err := work()
		a.win.Update(func() { u.notesLoaded(notes, total, err) })
	}()
}

// notesLoaded 收下一次更新日志的结果。
func (u *updateState) notesLoaded(notes []update.Note, total int, err error) {
	u.notesLoading = false
	if err != nil {
		u.notesErr = err.Error()
		return
	}
	u.notes, u.notesTotal = notes, total
}

// noteUpgrade 在启动时判断这次是不是刚从旧版本升上来的。
//
// 是的话把这段时间的日志取好，侧栏那张「已更新」卡片只在这一次启动里出现；
// 然后把当前版本写进配置——下次启动就没有「之前」了。
func (a *App) noteUpgrade() {
	last := a.cfg.LastVersion
	if last != "" && last != appVersion && update.Newer(appVersion, last) {
		a.upd.updatedTo = appVersion
		a.upd.status = fmt.Sprintf(i18n.T("update.banner.updated"), "v"+appVersion)
		a.loadNotes(last, appVersion)
	}
	if last != appVersion {
		a.cfg.LastVersion = appVersion
		a.commit()
	}
}

// updateBannerText 返回侧栏那张卡片的标题与副标题，空标题表示不显示。
func (a *App) updateBannerText() (string, string) {
	u := &a.upd
	switch {
	case u.rel != nil:
		sub := i18n.T("update.banner.view")
		if u.ready != "" {
			sub = i18n.T("update.banner.ready")
		}
		return fmt.Sprintf(i18n.T("update.banner.new"), "v"+u.rel.Version), sub
	case u.updatedTo != "":
		return fmt.Sprintf(i18n.T("update.banner.updated"), "v"+u.updatedTo), i18n.T("update.banner.view")
	}
	return "", ""
}

// updateBanner 是侧栏底部那张卡片：有新版本，或者这次启动刚更新过。
//
// 放在「设置」上面：它属于「这台机器」这一层，不在文稿树里。
func (a *App) updateBanner(c *ui.Context, pal chrome) {
	title, sub := a.updateBannerText()
	if title == "" {
		return
	}
	card := ui.ButtonBase(c).Padding(8, 10).Radius(8).FillWidth().Cursor(ui.CursorPointer).
		Label(title + " · " + sub)
	card.Background(pal.selectColor)
	if card.Hovered() {
		card.Background(pal.tabHover)
	}
	card.OnClick(func() { a.upd.overlay = true })
	card.Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(12).Bold().TextColor(pal.text).SingleLine().Ellipsis("…")
			ui.Text(c, sub).FontSize(10.5).TextColor(pal.muted).SingleLine().Ellipsis("…")
		})
	})
}

// notesView 是更新日志浮层：把 (旧版本, 新版本] 之间每一版的日志连起来看。
func (a *App) notesView(c *ui.Context) {
	u := &a.upd
	if !u.overlay {
		return
	}
	pal := chromeFor(a.tokens)
	t := c.Theme()
	ui.Modal(c, &u.overlay, func() {
		ui.Column(c).Width(760).Height(600).Radius(14).Background(pal.card).Children(func() {
			ui.Row(c).Height(46).Padding(0, 16, 0, 18).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Gap(1).Children(func() {
					ui.Text(c, i18n.T("update.notes.title")).FontSize(14).Bold().TextColor(pal.text)
					ui.Text(c, a.notesSubtitle()).FontSize(10.5).TextColor(pal.muted).SingleLine().Ellipsis("…")
				})
				iconButton(c, pal, iconClose, i18n.T("common.close"), func() { u.overlay = false })
			})
			ui.Box(c).Height(1).Background(pal.hairline)
			ui.Scroll(c).Grow(1).MinWidth(0).Children(func() {
				ui.Column(c).Padding(18, 16).Gap(18).Children(func() {
					switch {
					case u.notesLoading:
						ui.Text(c, i18n.T("update.notes.loading")).FontSize(11.5).TextColor(t.TextMuted)
					case u.notesErr != "":
						ui.Text(c, fmt.Sprintf(i18n.T("update.notes.failed"), u.notesErr)).
							FontSize(11.5).TextColor(t.TextMuted)
					case len(u.notes) == 0:
						ui.Text(c, i18n.T("update.notes.empty")).FontSize(11.5).TextColor(t.TextMuted)
					}
					for _, n := range u.notes {
						a.noteBlock(c, t, n)
					}
					if older := u.notesTotal - len(u.notes); older > 0 {
						ui.Text(c, fmt.Sprintf(i18n.T("update.notes.more"), len(u.notes), older)).
							FontSize(10.5).TextColor(t.TextMuted)
					}
				})
			})
			ui.Box(c).Height(1).Background(pal.hairline)
			ui.Row(c).Height(46).Padding(0, 16).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, a.updateHint()).FontSize(11).TextColor(pal.muted).Grow(1).SingleLine().Ellipsis("…")
				a.updateButton(c)
				if ui.Button(c, i18n.T("common.close")).Clicked() {
					u.overlay = false
				}
			})
		})
	})
}

// notesSubtitle 是浮层标题下面那行：从哪个版本看到哪个版本，共几版。
func (a *App) notesSubtitle() string {
	u := &a.upd
	from, to := u.notesAfter, appVersion
	if u.rel != nil {
		to = u.rel.Version
	}
	if from == "" {
		from = appVersion
	}
	count := u.notesTotal
	if count == 0 {
		count = len(u.notes)
	}
	if count == 0 {
		return ""
	}
	return fmt.Sprintf(i18n.T("update.notes.subtitle"), "v"+from, "v"+to, count)
}

// noteLang 是取更新日志用的语言："zh" 或 "en"，跟着界面走。
func noteLang() string {
	if i18n.English() {
		return "en"
	}
	return "zh"
}

// noteBlock 画一个版本：版本号一行，正文按行摊开。
//
// 正文存的是双语原文（英文在上、`<!-- lang:zh -->` 之下是中文），在这里按当前界面
// 语言切——所以切换语言时浮层立刻跟着变，不用重新去拉一遍。
func (a *App) noteBlock(c *ui.Context, t *ui.Theme, n update.Note) {
	ui.Column(c).Gap(5).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "v"+n.Version).FontSize(13).Bold().TextColor(t.Text)
			if n.Version == appVersion {
				ui.Text(c, i18n.T("update.notes.current")).FontSize(9.5).TextColor(t.TextMuted)
			}
		})
		for _, line := range noteLines(update.InLang(n.Notes, noteLang())) {
			switch {
			case line.Text == "":
				ui.Box(c).Height(3)
			case line.Heading:
				// 小标题要压过正文，否则整段读起来是一坨
				ui.Text(c, line.Text).FontSize(12).Bold().TextColor(t.Text)
			default:
				ui.Text(c, line.Text).FontSize(11.5).TextColor(t.TextMuted)
			}
		}
	})
}

// noteLine 是摊平之后的一行：文本，以及它原本是不是小标题。
type noteLine struct {
	Text    string
	Heading bool
}

// noteLines 把 release 正文压成几行纯文本。
//
// 更新日志浮层是自己排的字，塞不进 Markdown 渲染器；这里只做最小处理：
// 标题留字去 #（另外打个标记，画的时候加粗）、列表换成 •、链接留文字、
// ** 与 ` 去掉、PR 地址缩成 #123。空行只留一个，末尾的空白行去掉。
func noteLines(md string) []noteLine {
	var out []noteLine
	blank := true
	for _, line := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			if !blank {
				out = append(out, noteLine{})
			}
			blank = true
			continue
		}
		blank = false
		trimmed := strings.TrimSpace(line)
		heading := false
		switch {
		case strings.HasPrefix(trimmed, "#"):
			line = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			heading = true
		case noteBullet.MatchString(line):
			line = "• " + noteBullet.ReplaceAllString(line, "")
		default:
			line = trimmed
		}
		line = noteLink.ReplaceAllString(line, "$1")
		line = noteMark.ReplaceAllString(line, "")
		line = strings.TrimSpace(noteBare.ReplaceAllStringFunc(line, func(u string) string {
			// GitHub 自动生成的条目以 PR 地址结尾，序号比网址好读
			if i := strings.LastIndex(u, "/pull/"); i >= 0 {
				return "#" + u[i+len("/pull/"):]
			}
			return u
		}))
		out = append(out, noteLine{Text: line, Heading: heading})
	}
	for len(out) > 0 && out[len(out)-1].Text == "" {
		out = out[:len(out)-1]
	}
	return out
}
