module github.com/carn181/faustlsp

go 1.24.3

require (
	github.com/fsnotify/fsnotify v1.9.0
	github.com/khiner/tree-sitter-faust v0.0.0-20250701002309-122dd1019192
	github.com/otiai10/copy v1.14.1
	github.com/tree-sitter/go-tree-sitter v0.25.0
)

require (
	github.com/mattn/go-pointer v0.0.1 // indirect
	github.com/otiai10/mint v1.6.3 // indirect
	golang.org/x/sync v0.8.0 // indirect
	golang.org/x/sys v0.24.0 // indirect
)

replace github.com/fsnotify/fsnotify v1.9.0 => github.com/carn181/fsnotify v0.0.0-20250612182652-935ca6b92412
