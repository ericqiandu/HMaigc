package service

// agentCanvasPosition is the shared deterministic projection coordinate used
// by canvas.commit. Canvas facts are frozen in each model task context instead
// of being exposed through a parallel low-level read tool.
type agentCanvasPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
