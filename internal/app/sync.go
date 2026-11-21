package app

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"papero/internal/i18n"
	"papero/internal/sync"
)

// newEngine 按当前设置装配一次同步：远端 + 用户选的忽略规则。
//
// .gitignore 每次同步现读：改了规则不用重启。
func (a *App) newEngine(remote sync.Remote) *sync.Engine {
	return &sync.Engine{
		Root:   a.vault.Root(),
		Remote: remote,
		Ignore: sync.NewFilter(a.vault.Root(), a.cfg.Sync.IgnoreDot, a.cfg.Sync.IgnoreGitignore),
	}
}

// buildRemote 按设置构造同步远端。
func (a *App) buildRemote() sync.Remote {
	cfg := a.cfg.Sync
	switch cfg.Remote {
	case "git":
		return &sync.GitRemote{
			Repo:   cfg.Repo,
			Branch: cfg.Branch,
			Token:  cfg.Token,
			Dir:    filepath.Join(a.vault.Root(), ".papero", "git"),
		}
	case "webdav":
		return &sync.WebDAVRemote{Base: cfg.URL, User: cfg.User, Pass: cfg.Password}
	case "repo":
		// 库自己的仓库：不另建副本，也不填令牌
		return &sync.RepoRemote{Root: a.vault.Root(), Branch: ""}
	default:
		return &sync.DirRemote{Root: cfg.Dir}
	}
}

// testConnection 探一次远端：地址对不对、能不能登录、远端有多少文件。
//
// 不要求同步已经启用——用户通常是打开同步之前先试一下。没有真窗口时（测试、
// -shot）同步跑完再写状态，否则结果永远回不来。
func (a *App) testConnection() {
	if a.syncing || a.probing {
		return
	}
	cfg := a.cfg.Sync
	switch {
	case cfg.Remote == "git" && cfg.Repo == "":
		a.syncMsg = i18n.T("sync.need_repo")
		return
	case cfg.Remote == "webdav" && cfg.URL == "":
		a.syncMsg = i18n.T("sync.need_address")
		return
	case cfg.Remote != "git" && cfg.Remote != "webdav" && cfg.Remote != "repo" && cfg.Dir == "":
		a.syncMsg = i18n.T("sync.need_folder")
		return
	}
	a.probing = true
	a.syncMsg = i18n.T("sync.testing")
	remote := a.buildRemote()
	probe := func() string {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := sync.Probe(ctx, remote)
		if err != nil {
			return i18n.T("sync.test_failed") + err.Error()
		}
		switch {
		case res.New:
			return i18n.T("sync.test_new")
		case res.Files >= 0:
			return fmt.Sprintf(i18n.T("sync.test_files"), res.Files)
		case res.Detail != "":
			return fmt.Sprintf(i18n.T("sync.test_repo"), res.Detail)
		default:
			return i18n.T("sync.test_ok")
		}
	}
	if a.win == nil {
		a.probing = false
		a.syncMsg = probe()
		return
	}
	go func() {
		msg := probe()
		a.win.Update(func() {
			a.probing = false
			a.syncMsg = msg
		})
	}()
}

// pullCloud 从云端拉一遍：只下载，不上传、不删除。
//
// manual 是用户按的按钮——没更新也回一句；开机自动拉的那次没变化就不打扰。
// 覆盖本地之前，引擎会把本地那一版存进 .papero/before-pull-<时间>/。
func (a *App) pullCloud(manual bool) {
	if a.syncing || a.pulling {
		return
	}
	cfg := a.cfg.Sync
	switch {
	case cfg.Remote == "git" && cfg.Repo == "":
		a.syncMsg = i18n.T("sync.need_repo")
		return
	case cfg.Remote == "webdav" && cfg.URL == "":
		a.syncMsg = i18n.T("sync.need_address")
		return
	case cfg.Remote != "git" && cfg.Remote != "webdav" && cfg.Remote != "repo" && cfg.Dir == "":
		a.syncMsg = i18n.T("sync.need_folder")
		return
	}
	a.pulling = true
	a.syncMsg = i18n.T("sync.pulling")
	remote := a.buildRemote()

	pull := func() (sync.Result, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		return a.newEngine(remote).Pull(ctx)
	}
	finish := func(res sync.Result, err error) string {
		if err != nil {
			return i18n.T("sync.failed") + err.Error()
		}
		if res.Downloaded == 0 && res.Pending == 0 && len(res.Failed) == 0 && res.Stopped == "" {
			if !manual {
				return ""
			}
			return i18n.T("sync.pull_none")
		}
		msg := fmt.Sprintf(i18n.T("sync.pull_done"), res.Downloaded)
		if res.Pending > 0 {
			msg += " · " + fmt.Sprintf(i18n.T("sync.pull_pending"), res.Pending)
		}
		if len(res.Failed) > 0 {
			msg += "：" + res.Failed[0].Reason
		}
		if res.Stopped != "" {
			msg += " · " + res.Stopped
		}
		return msg
	}

	// 没有真窗口（测试、-shot）时同步跑完，结果才回得来
	if a.win == nil {
		res, err := pull()
		a.pulling = false
		a.syncMsg = finish(res, err)
		if err == nil {
			a.refresh()
			a.reloadPulled(res.Updated)
		}
		return
	}
	go func() {
		res, err := pull()
		a.win.Update(func() {
			a.pulling = false
			if msg := finish(res, err); msg != "" {
				a.syncMsg = msg
			}
			if err == nil {
				a.refresh()
				a.reloadPulled(res.Updated)
			}
		})
	}()
}

// pullOnOpen 是设置里的「打开时先从云端拉取一遍」。
func (a *App) pullOnOpen() {
	if !a.cfg.Sync.Enabled || !a.cfg.Sync.PullOnOpen {
		return
	}
	a.pullCloud(false)
}

// reloadPulled 把这次从云端写下来的、正开着的文稿重新读一遍。
//
// 有未保存改动的跳过：用户正在写的内容不能被一次后台拉取冲掉。它们会被留下
// 一次同步去合并（那边会保留本地副本），所以不会丢。
func (a *App) reloadPulled(paths []string) {
	if len(paths) == 0 {
		return
	}
	updated := make(map[string]bool, len(paths))
	for _, p := range paths {
		updated[p] = true
	}
	skipped := 0
	for _, t := range a.tabs {
		if !updated[t.rel] {
			continue
		}
		if t.dirty() {
			skipped++
			continue
		}
		a.load(t)
	}
	if skipped > 0 {
		a.say(fmt.Sprintf(i18n.T("sync.pull_dirty"), skipped))
	}
}

// runSync 在后台跑一次同步，结果写回状态栏。
func (a *App) runSync() {
	if a.syncing || a.probing {
		return
	}
	if !a.cfg.Sync.Enabled {
		a.syncMsg = i18n.T("sync.disabled")
		return
	}
	cfg := a.cfg.Sync
	if cfg.Remote == "dir" && cfg.Dir == "" {
		a.syncMsg = i18n.T("sync.need_folder")
		return
	}
	if cfg.Remote == "git" && cfg.Repo == "" {
		a.syncMsg = i18n.T("sync.need_repo")
		return
	}
	if cfg.Remote == "webdav" && cfg.URL == "" {
		a.syncMsg = i18n.T("sync.need_address")
		return
	}

	a.syncing = true
	a.syncMsg = i18n.T("sync.running")
	a.saveAll()
	remote := a.buildRemote()
	engine := a.newEngine(remote)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		res, err := engine.Run(ctx)
		msg := ""
		if err != nil {
			msg = i18n.T("sync.failed") + err.Error()
		} else {
			msg = res.Summary()
			if len(res.Conflicts) > 0 {
				msg += i18n.T("sync.conflict_kept")
			}
			// 没同步成的，说清第一个为什么：状态栏放不下全部，但放得下一条原因
			if len(res.Failed) > 0 {
				msg += "：" + res.Failed[0].Reason
			}
			if res.Stopped != "" {
				msg += " · " + res.Stopped
			}
			// 第一次接上一个已经有文件的远端：本地那一版整批收在一个目录里，
			// 状态栏只说本地版本存到了哪。
			if res.JoinDir != "" {
				msg += " · " + fmt.Sprintf(i18n.T("sync.join_kept"), res.JoinDir)
			}
		}
		if a.win == nil {
			return
		}
		a.win.Update(func() {
			a.syncing = false
			a.syncMsg = msg
			if err == nil {
				// 远端拉下来的文件要出现在目录树里。
				a.refresh()
			}
		})
	}()
}
