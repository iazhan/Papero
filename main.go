// Papero 是一个以 Markdown 为主、但不止 Markdown 的个人知识库。
//
//	go run .                          启动
//	go run . -shot 预览.png            把界面渲染成图片（开发辅助）
package main

import (
	"flag"
	"log"

	"papero/internal/app"
)

func main() {
	shot := flag.String("shot", "", "把界面渲染成 PNG 后退出，用于检查外观")
	open := flag.String("open", "", "配合 -shot：打开知识库外的某个文件")
	graph := flag.Bool("graph", false, "配合 -shot：渲染独立的知识图谱窗口")
	width := flag.Int("width", 1180, "渲染宽度")
	height := flag.Int("height", 820, "渲染高度")
	scale := flag.Float64("scale", 2, "渲染缩放")
	tabs := flag.Int("tabs", 1, "配合 -shot：打开多少个标签，用来检查标签栏溢出")
	flag.Parse()

	if *shot != "" {
		var err error
		if *graph {
			err = app.ShotGraph(*shot, *width, *height, float32(*scale))
		} else if *open != "" {
			err = app.ShotFile(*shot, *open, *width, *height, float32(*scale))
		} else {
			err = app.Shot(*shot, *width, *height, float32(*scale), *tabs)
		}
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	app.Run()
}
