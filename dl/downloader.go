package dl

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/melbahja/got"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
	"golang.org/x/sync/errgroup"

	"onedrive-dl/od"
)

type DL struct {
	OutDir      string
	Jobs        int
	ConnPerFile int
	Debug       bool
	Client      *http.Client
}

type Result struct {
	File    od.FileEntry
	Skipped bool
	Err     error
}

func New(outDir string, jobs, connPerFile int, debug bool, client *http.Client) *DL {
	if connPerFile < 1 {
		connPerFile = 1
	}
	return &DL{
		OutDir:      outDir,
		Jobs:        jobs,
		ConnPerFile: connPerFile,
		Debug:       debug,
		Client:      client,
	}
}

func (d *DL) Start(info *od.ShareInfo) []Result {
	results := make([]Result, len(info.Files))

	fmt.Printf("\nDownloading %d files (%s), %d parallel, %d conn/file\n",
		info.TotalFiles, fmtSize(info.TotalSize), d.Jobs, d.ConnPerFile)
	fmt.Printf("Output: %s\n\n", d.OutDir)

	if err := os.MkdirAll(d.OutDir, 0755); err != nil {
		for i := range results {
			results[i] = Result{File: info.Files[i], Err: fmt.Errorf("mkdir: %w", err)}
		}
		return results
	}

	p := mpb.New(mpb.WithWidth(64))

	g, _ := errgroup.WithContext(context.Background())
	g.SetLimit(d.Jobs)

	for i, file := range info.Files {
		i, file := i, file
		g.Go(func() error {
			results[i] = d.dlFile(file, p)
			return nil
		})
	}

	g.Wait()
	p.Wait()

	return results
}

func (d *DL) dlFile(file od.FileEntry, p *mpb.Progress) Result {
	localPath := filepath.Join(d.OutDir, filepath.FromSlash(file.RelPath))

	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return Result{File: file, Err: fmt.Errorf("mkdir: %w", err)}
	}

	if stat, err := os.Stat(localPath); err == nil && stat.Size() == file.Size {
		return Result{File: file, Skipped: true}
	}

	if file.DlURL == "" {
		return Result{File: file, Err: fmt.Errorf("no download URL")}
	}

	name := file.Name
	if len(name) > 30 {
		name = "..." + name[len(name)-27:]
	}
	bar := p.AddBar(file.Size,
		mpb.PrependDecorators(
			decor.Name(name, decor.WC{W: 30}),
			decor.CountersKibiByte("% .2f / % .2f"),
		),
		mpb.AppendDecorators(
			decor.EwmaETA(decor.ET_STYLE_GO, 60),
			decor.Name(" "),
			decor.EwmaSpeed(decor.SizeB1024(0), "% .2f", 60),
		),
	)

	dl := got.NewDownload(context.Background(), file.DlURL, localPath)
	dl.Client = d.Client
	dl.Concurrency = uint(d.ConnPerFile)
	dl.Header = []got.GotHeader{
		{Key: "User-Agent", Value: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Mobile/15E148 Safari/604.1"},
	}

	if err := dl.Init(); err != nil {
		bar.Abort(true)
		return Result{File: file, Err: fmt.Errorf("init: %w", err)}
	}

	done := make(chan struct{})
	go func() {
		var last int64
		ticker := time.NewTicker(150 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				cur := int64(dl.Size())
				if delta := cur - last; delta > 0 {
					bar.IncrBy(int(delta))
					last = cur
				}
			}
		}
	}()

	err := dl.Start()
	close(done)

	if err != nil {
		bar.Abort(true)
		return Result{File: file, Err: fmt.Errorf("download: %w", err)}
	}

	bar.SetCurrent(file.Size)
	return Result{File: file}
}

func Summary(results []Result) {
	var ok, skip, fail int
	for _, r := range results {
		switch {
		case r.Err != nil:
			fail++
		case r.Skipped:
			skip++
		default:
			ok++
		}
	}

	fmt.Printf("\n--- Download Summary ---\n")
	fmt.Printf("Succeeded: %d\n", ok)
	if skip > 0 {
		fmt.Printf("Skipped (already exists): %d\n", skip)
	}
	if fail > 0 {
		fmt.Printf("Failed: %d\n", fail)
		for _, r := range results {
			if r.Err != nil {
				fmt.Printf("  x %s: %v\n", r.File.RelPath, r.Err)
			}
		}
	}
}

func fmtSize(bytes int64) string {
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
