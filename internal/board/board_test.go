package board

import (
	"encoding/json"
	"strings"
	"testing"
)

const sample = `{
  "type": "excalidraw",
  "version": 2,
  "elements": [
    {"id": "r1", "type": "rectangle", "x": 10, "y": 20, "width": 100, "height": 50,
     "strokeColor": "#1e1e1e", "backgroundColor": "transparent", "strokeWidth": 2},
    {"id": "t1", "type": "text", "x": 30, "y": 30, "width": 60, "height": 20,
     "text": "你好", "fontSize": 18, "strokeColor": "#1e1e1e"}
  ],
  "appState": {"viewBackgroundColor": "#ffffff"},
  "files": {}
}`

func TestParseAndEncode(t *testing.T) {
	scene, err := Parse([]byte(sample))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(scene.Elements) != 2 {
		t.Fatalf("元素数 = %d，想要 2", len(scene.Elements))
	}
	if scene.Elements[1].Text != "你好" {
		t.Errorf("文字 = %q", scene.Elements[1].Text)
	}

	out, err := scene.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var round map[string]any
	if err := json.Unmarshal(out, &round); err != nil {
		t.Fatalf("输出不是合法 JSON：%v", err)
	}
	if round["type"] != "excalidraw" || round["source"] != "papero" {
		t.Errorf("输出头部 = %v / %v", round["type"], round["source"])
	}
	if !strings.Contains(string(out), "你好") {
		t.Error("中文文字没有原样写回")
	}
}

func TestParseRejectsWrongType(t *testing.T) {
	if _, err := Parse([]byte(`{"type":"other"}`)); err == nil {
		t.Fatal("类型不对时应当报错")
	}
	if _, err := Parse([]byte("not json")); err == nil {
		t.Fatal("非 JSON 应当报错")
	}
}

func TestAddRemoveFind(t *testing.T) {
	scene, _ := Parse([]byte(sample))
	el := scene.Add("ellipse", 200, 200)
	if el.ID == "" || el.Type != "ellipse" {
		t.Fatalf("新元素 = %+v", el)
	}
	if scene.Find(el.ID) == nil {
		t.Error("找不到新元素")
	}
	scene.Remove("r1")
	if scene.Find("r1") != nil {
		t.Error("删除后仍能找到")
	}
	if len(scene.Visible()) != 2 {
		t.Errorf("可见元素 = %d，想要 2", len(scene.Visible()))
	}
}

func TestHit(t *testing.T) {
	scene, _ := Parse([]byte(sample))
	if el := scene.Hit(50, 40); el == nil || el.ID != "t1" {
		t.Errorf("命中 = %+v，想要文字元素", el)
	}
	if el := scene.Hit(500, 500); el != nil {
		t.Errorf("空白处不应命中，得到 %+v", el)
	}
}

func TestBounds(t *testing.T) {
	scene, _ := Parse([]byte(sample))
	minX, minY, maxX, maxY := scene.Bounds()
	if minX != 10 || minY != 20 || maxX != 110 || maxY != 70 {
		t.Errorf("外接矩形 = %v,%v,%v,%v", minX, minY, maxX, maxY)
	}
}
