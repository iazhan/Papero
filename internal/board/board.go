// Package board 是白板的数据层：读写 Excalidraw 场景文件（.excalidraw）。
//
// 它只处理数据：解析 JSON、增删元素、保存回原格式。绘制在 internal/app 里
// 用 MyGo 的 Painter 完成，所以白板是原生绘制的，不需要 WebView。
//
// 白板是 Papero 的内置功能，不是插件：它要画布、要和文稿列表、标签、
// 保存流程长在一起，编译进同一个二进制最省事，也省掉一层没有意义的间接。
//
// 文件格式与 Excalidraw 兼容：Papero 存的 .excalidraw 可以拖进 Excalidraw 打开，
// Excalidraw 存的也能在这里打开。只实现常用元素：矩形、椭圆、菱形、
// 直线、箭头、手绘与文字。
package board

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"time"
)

// Scene 是 .excalidraw 文件的内容。
type Scene struct {
	Type     string          `json:"type"`
	Version  int             `json:"version"`
	Source   string          `json:"source"`
	Elements []Element       `json:"elements"`
	AppState map[string]any  `json:"appState"`
	Files    map[string]File `json:"files"`
}

// Element 是白板上的一个元素，字段与 Excalidraw 一致。
type Element struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Width       float64 `json:"width"`
	Height      float64 `json:"height"`
	Angle       float64 `json:"angle"`
	Stroke      string  `json:"strokeColor"`
	Fill        string  `json:"backgroundColor"`
	FillStyle   string  `json:"fillStyle"`
	StrokeWidth float64 `json:"strokeWidth"`
	Opacity     float64 `json:"opacity"`
	Roughness   int     `json:"roughness"`
	Text        string  `json:"text,omitempty"`
	FontSize    float64 `json:"fontSize,omitempty"`
	// Points 是直线、箭头与手绘的顶点，相对元素左上角。
	Points [][]float64 `json:"points,omitempty"`
	// Seed 决定手绘抖动的形态；同一份文件重开时抖动不变。
	Seed int64 `json:"seed,omitempty"`
	// StartBinding / EndBinding 记录箭头两端粘在哪个形状上（Excalidraw 同名字段）。
	StartBinding *Binding `json:"startBinding,omitempty"`
	EndBinding   *Binding `json:"endBinding,omitempty"`
	// GroupIDs 是元素所属的分组（Excalidraw 同名字段，数组是为了嵌套分组）。
	GroupIDs []string `json:"groupIds,omitempty"`
	// FileID 指向 Scene.Files 里的一张图（Excalidraw 同名字段）。
	FileID string `json:"fileId,omitempty"`
	// ContainerID 让文字元素知道自己贴在哪个形状里（Excalidraw 同名字段）。
	ContainerID string `json:"containerId,omitempty"`
	// BoundElements 是形状上挂着的元素（文字），与 ContainerID 互相指向。
	BoundElements []BoundElement `json:"boundElements,omitempty"`
	Deleted       bool           `json:"isDeleted,omitempty"`
	Version       int            `json:"version"`
	// 其余字段（绑定、分组、种子等）原样保留，保证与 Excalidraw 互通。
	Extra map[string]json.RawMessage `json:"-"`
}

// File 是白板里嵌入的图片（Excalidraw 把二进制存成 dataURL）。
type File struct {
	ID       string `json:"id,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	DataURL  string `json:"dataURL,omitempty"`
	Created  int64  `json:"created,omitempty"`
	Version  int    `json:"version,omitempty"`
}

// Binding 是箭头一端与形状的绑定。
type Binding struct {
	ElementID string  `json:"elementId"`
	Focus     float64 `json:"focus,omitempty"`
	Gap       float64 `json:"gap,omitempty"`
}

// 默认配色，与 Excalidraw 的默认色板一致。
const (
	DefaultStroke = "#1e1e1e"
	DefaultFill   = "transparent"
)

// Parse 解析 .excalidraw。
func Parse(data []byte) (*Scene, error) {
	var scene Scene
	if err := json.Unmarshal(data, &scene); err != nil {
		return nil, errors.New("白板：不是有效的 .excalidraw 文件")
	}
	if scene.Type != "" && scene.Type != "excalidraw" {
		return nil, errors.New("白板：文件类型不是 excalidraw")
	}
	if scene.AppState == nil {
		scene.AppState = map[string]any{}
	}
	if scene.Files == nil {
		scene.Files = map[string]File{}
	}
	return &scene, nil
}

// Encode 把场景写回 JSON。
func (s *Scene) Encode() ([]byte, error) {
	s.Type = "excalidraw"
	if s.Version == 0 {
		s.Version = 2
	}
	if s.Source == "" {
		s.Source = "papero"
	}
	return json.MarshalIndent(s, "", "  ")
}

// Add 在 (x, y) 处新建一个元素。
//
// 返回的指针只在**下一次 Add 之前**有效：Add 里的 append 可能让底层数组搬家，
// 之后请用 Find(id) 重新取。

func (s *Scene) Add(kind string, x, y float64) *Element {
	el := Element{
		ID:          newID(),
		Type:        kind,
		X:           x,
		Y:           y,
		Width:       160,
		Height:      100,
		Stroke:      DefaultStroke,
		Fill:        DefaultFill,
		FillStyle:   "hachure",
		StrokeWidth: 2,
		Opacity:     100,
		Roughness:   1,
		Version:     1,
		Seed:        newSeed(),
	}
	switch kind {
	case "text":
		el.Width, el.Height = 200, 28
		el.Text = "文字"
		el.FontSize = 20
		el.Fill = "transparent"
	case "arrow", "line":
		el.Width, el.Height = 160, 0
		el.Points = [][]float64{{0, 0}, {160, 0}}
	}
	s.Elements = append(s.Elements, el)
	return &s.Elements[len(s.Elements)-1]
}

// Clone 深拷贝全部元素（撤销栈用）。
func (s *Scene) Clone() []Element {
	out := make([]Element, len(s.Elements))
	for i, el := range s.Elements {
		cp := el
		if el.Points != nil {
			cp.Points = make([][]float64, len(el.Points))
			for j, pt := range el.Points {
				cp.Points[j] = append([]float64(nil), pt...)
			}
		}
		if el.Extra != nil {
			cp.Extra = make(map[string]json.RawMessage, len(el.Extra))
			for k, v := range el.Extra {
				cp.Extra[k] = v
			}
		}
		out[i] = cp
	}
	return out
}

// Restore 用一份快照替换全部元素。
func (s *Scene) Restore(elems []Element) {
	s.Elements = elems
}

// Remove 删除一个元素。
func (s *Scene) Remove(id string) {
	out := s.Elements[:0]
	for _, el := range s.Elements {
		if el.ID != id || el.Deleted {
			out = append(out, el)
		}
	}
	s.Elements = out
}

// Find 按 id 找元素。
func (s *Scene) Find(id string) *Element {
	for i := range s.Elements {
		if s.Elements[i].ID == id && !s.Elements[i].Deleted {
			return &s.Elements[i]
		}
	}
	return nil
}

// Visible 返回没有删除的元素。
func (s *Scene) Visible() []Element {
	out := make([]Element, 0, len(s.Elements))
	for _, el := range s.Elements {
		if !el.Deleted {
			out = append(out, el)
		}
	}
	return out
}

// Hit 返回点 (x, y) 命中的最上层元素；坐标与元素同一坐标系。
func (s *Scene) Hit(x, y float64) *Element {
	for i := len(s.Elements) - 1; i >= 0; i-- {
		el := &s.Elements[i]
		if el.Deleted {
			continue
		}
		pad := 6.0
		if el.Type == "line" || el.Type == "arrow" || el.Type == "freedraw" {
			for _, p := range el.Points {
				if len(p) >= 2 && math.Hypot(el.X+p[0]-x, el.Y+p[1]-y) < 12 {
					return el
				}
			}
			continue
		}
		if x >= el.X-pad && x <= el.X+el.Width+pad && y >= el.Y-pad && y <= el.Y+el.Height+pad {
			return el
		}
	}
	return nil
}

// newID 生成 Excalidraw 风格的随机 id。
// newSeed 返回手绘抖动用的随机种子。
func newSeed() int64 {
	var b [8]byte
	rand.Read(b[:])
	var v int64
	for _, c := range b {
		v = v<<8 | int64(c)
	}
	if v < 0 {
		v = -v
	}
	return v
}

func newID() string {
	var b [12]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:]) + "-" + time.Now().Format("150405")
}

// Bounds 返回所有元素的外接矩形，用于「适应窗口」。
func (s *Scene) Bounds() (minX, minY, maxX, maxY float64) {
	first := true
	for _, el := range s.Visible() {
		x0, y0, x1, y1 := el.X, el.Y, el.X+el.Width, el.Y+el.Height
		if el.Type == "line" || el.Type == "arrow" || el.Type == "freedraw" {
			for _, p := range el.Points {
				if len(p) < 2 {
					continue
				}
				x0 = math.Min(x0, el.X+p[0])
				x1 = math.Max(x1, el.X+p[0])
				y0 = math.Min(y0, el.Y+p[1])
				y1 = math.Max(y1, el.Y+p[1])
			}
		}
		if first {
			minX, minY, maxX, maxY = x0, y0, x1, y1
			first = false
			continue
		}
		minX = math.Min(minX, x0)
		minY = math.Min(minY, y0)
		maxX = math.Max(maxX, x1)
		maxY = math.Max(maxY, y1)
	}
	return
}
