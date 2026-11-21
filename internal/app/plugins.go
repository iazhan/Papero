package app

// isPluginDisabled 报告插件是否被用户停用。
func (a *App) isPluginDisabled(id string) bool {
	for _, d := range a.cfg.DisabledPlugins {
		if d == id {
			return true
		}
	}
	return false
}

// setPluginEnabled 启用或停用一个插件，并写入设置。
func (a *App) setPluginEnabled(id string, on bool) {
	out := a.cfg.DisabledPlugins[:0]
	for _, d := range a.cfg.DisabledPlugins {
		if d != id {
			out = append(out, d)
		}
	}
	if !on {
		out = append(out, id)
	}
	a.cfg.DisabledPlugins = out
	a.cfg.Save()
}
