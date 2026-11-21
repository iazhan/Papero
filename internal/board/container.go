package board

import "strings"

// 形状上的文字：Excalidraw 的做法是「文字元素 + containerId 指向形状」，
// 形状本身在 boundElements 里记一笔。这里照抄同样的结构，文件仍然互通。
//
// 文字始终居中在形状里：形状移动、缩放、改文字之后都调一次 CenterContainerText。

// BoundElement 是形状上挂着的元素（目前只有文字）。
type BoundElement struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// ContainerText 返回某个形状上的文字元素，没有则返回 nil。
func (s *Scene) ContainerText(shapeID string) *Element {
	for i := range s.Elements {
		el := &s.Elements[i]
		if el.Deleted || el.Type != "text" {
			continue
		}
		if el.ContainerID == shapeID {
			return el
		}
	}
	return nil
}

// EnsureContainerText 给形状配一段文字：有就返回，没有就建一个空白的。
func (s *Scene) EnsureContainerText(shape *Element) *Element {
	if t := s.ContainerText(shape.ID); t != nil {
		return t
	}
	size := 20.0
	t := Element{
		ID:          newID(),
		Type:        "text",
		Text:        "",
		FontSize:    size,
		Stroke:      DefaultStroke,
		Fill:        "transparent",
		StrokeWidth: 1,
		Opacity:     100,
		Version:     1,
		Seed:        newSeed(),
		ContainerID: shape.ID,
	}
	s.Elements = append(s.Elements, t)
	// append 可能让底层数组搬家，按 ID 重新取
	text := s.Find(t.ID)
	shape.BoundElements = append(shape.BoundElements, BoundElement{Type: "text", ID: t.ID})
	s.CenterContainerText(shape)
	return text
}

// CenterContainerText 把形状上的文字摆到正中。
func (s *Scene) CenterContainerText(shape *Element) {
	text := s.ContainerText(shape.ID)
	if text == nil {
		return
	}
	size := text.FontSize
	if size <= 0 {
		size = 20
	}
	w, h := TextBlockSize(text.Text, size)
	text.X = shape.X + (shape.Width-w)/2
	text.Y = shape.Y + (shape.Height-h)/2
}

// LineHeight 是一行文字占的高度（相对字号）。
const LineHeight = 1.25

// TextWidth 估算一行文字的宽度：汉字按一个字号宽，西文按 0.55。
// 没有排版引擎可问，只能估；居中够用了。
func TextWidth(s string, size float64) float64 {
	// 多行时取最宽的那一行
	best := 0.0
	for _, line := range strings.Split(s, "\n") {
		var w float64
		for _, r := range line {
			if r < 128 {
				w += 0.55
			} else {
				w += 1.0
			}
		}
		if w > best {
			best = w
		}
	}
	return best * size
}

// TextBlockSize 返回多行文字占的宽与高。
func TextBlockSize(s string, size float64) (float64, float64) {
	n := len(strings.Split(s, "\n"))
	return TextWidth(s, size), float64(n) * size * LineHeight
}

// RemoveWithContainer 删掉一个元素，连带它上面的文字（形状被删时用）。
func (s *Scene) RemoveWithContainer(id string) {
	if t := s.ContainerText(id); t != nil {
		s.Remove(t.ID)
	}
	s.Unbind(id)
	s.Remove(id)
}
