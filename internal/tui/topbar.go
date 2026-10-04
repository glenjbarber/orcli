package tui

// topBar renders the bar carrying what changes second by second, sweeping the Status
// value along the row while a turn runs.
//
// The sweep is applied after the bar is rendered rather than as a span inside it, since
// the bar renderers return one string and the hue has to change every step. The bar is
// rendered once with the plain state and swept only while a turn is running, so an idle
// frame is the same text a reader can select and copy.
func topBar(status string, running bool, step int) string {
	bar := RenderTop(status, "", "", "", "", "", "")
	if !running {
		return bar
	}
	return sweepStatus(bar, status, step)
}
