package main

import (
	"fmt"
	"sort"
	"strings"

	"onedrive-dl/od"
)

type node struct {
	name     string
	size     int64
	isDir    bool
	children []*node
}

func showTree(info *od.ShareInfo) {
	root := mkTree(info.Files)
	printNode(root, "", true)
	fmt.Printf("\n%d files, %s total\n", info.TotalFiles, fmtTreeSize(info.TotalSize))
}

func mkTree(files []od.FileEntry) *node {
	root := &node{name: ".", isDir: true}

	for _, f := range files {
		parts := strings.Split(f.RelPath, "/")
		cur := root
		for i, part := range parts {
			if i == len(parts)-1 {
				cur.children = append(cur.children, &node{
					name: part,
					size: f.Size,
				})
			} else {
				found := false
				for _, child := range cur.children {
					if child.isDir && child.name == part {
						cur = child
						found = true
						break
					}
				}
				if !found {
					dir := &node{name: part, isDir: true}
					cur.children = append(cur.children, dir)
					cur = dir
				}
			}
		}
	}

	sortKids(root)

	if len(root.children) == 1 && root.children[0].isDir {
		return root.children[0]
	}
	return root
}

func sortKids(n *node) {
	sort.Slice(n.children, func(i, j int) bool {
		a, b := n.children[i], n.children[j]
		if a.isDir != b.isDir {
			return a.isDir
		}
		return strings.ToLower(a.name) < strings.ToLower(b.name)
	})
	for _, child := range n.children {
		if child.isDir {
			sortKids(child)
		}
	}
}

func printNode(n *node, indent string, isRoot bool) {
	if isRoot {
		if n.isDir {
			fmt.Printf("%s/\n", n.name)
		} else {
			fmt.Printf("%s (%s)\n", n.name, fmtTreeSize(n.size))
		}
	}

	for i, child := range n.children {
		isLast := i == len(n.children)-1
		connector := "├── "
		if isLast {
			connector = "└── "
		}

		if child.isDir {
			fmt.Printf("%s%s%s/\n", indent, connector, child.name)
			nextIndent := indent + "│   "
			if isLast {
				nextIndent = indent + "    "
			}
			printNode(child, nextIndent, false)
		} else {
			fmt.Printf("%s%s%s (%s)\n", indent, connector, child.name, fmtTreeSize(child.size))
		}
	}
}

func fmtTreeSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
