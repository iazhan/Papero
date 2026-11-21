# Papero 的构建入口。
#
# Windows 上必须带 -H windowsgui：否则双击启动会先弹一个黑色终端窗口。

APP     := papero
OUT     := build/$(APP).exe

ifeq ($(OS),Windows_NT)
	LDFLAGS := -H windowsgui -s -w
else
	LDFLAGS := -s -w
endif

.PHONY: build run test fmt vet clean

build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o $(OUT) .

run: build
	./$(OUT)

test:
	go test ./...

fmt:
	gofmt -w internal

vet:
	go vet ./...

clean:
	rm -rf build
