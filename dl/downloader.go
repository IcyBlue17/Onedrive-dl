package dl

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
	"golang.org/x/sync/errgroup"

	"onedrive-dl/od"
)

type DL struct {
	OutDir string
	Jobs   int
	Debug  bool
	Client *http.Client
}

type Result struct {
	File    od.FileEntry
	Skipped bool
	Err     error
}

func New(outDir string, jobs int, debug bool, client *http.Client) *DL {
	return &DL{
		OutDir: outDir,
		Jobs:   jobs,
		Debug:  debug,
		Client: client,
	}
}

func (d *DL) Start(info *od.ShareInfo) []Result {
	results := make([]Result, len(info.Files))

	fmt.Printf("\nDownloading %d files (%s) with %d concurrent connections\n",
		info.TotalFiles, fmtSize(info.TotalSize), d.Jobs)
	fmt.Printf("Output directory: %s\n\n", d.OutDir)

	if err := os.MkdirAll(d.OutDir, 0755); err != nil {
		for i := range results {
			results[i] = Result{File: info.Files[i], Err: fmt.Errorf("failed to create output dir: %w", err)}
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
		return Result{File: file, Err: fmt.Errorf("mkdir failed: %w", err)}
	}

	if stat, err := os.Stat(localPath); err == nil {
		if stat.Size() == file.Size {
			return Result{File: file, Skipped: true}
		}
	}

	if file.DlURL == "" {
		return Result{File: file, Err: fmt.Errorf("no download URL")}
	}

	tmpPath := localPath + ".downloading"
	var offset int64
	if stat, err := os.Stat(tmpPath); err == nil {
		offset = stat.Size()
	}

	req, err := http.NewRequest("GET", file.DlURL, nil)
	if err != nil {
		return Result{File: file, Err: err}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	resp, err := d.Client.Do(req)
	if err != nil {
		return Result{File: file, Err: fmt.Errorf("request failed: %w", err)}
	}
	defer resp.Body.Close()

	var outFile *os.File
	if resp.StatusCode == http.StatusPartialContent && offset > 0 {
		outFile, err = os.OpenFile(tmpPath, os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return Result{File: file, Err: err}
		}
	} else if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent {
		offset = 0
		outFile, err = os.Create(tmpPath)
		if err != nil {
			return Result{File: file, Err: err}
		}
	} else {
		return Result{File: file, Err: fmt.Errorf("HTTP %d", resp.StatusCode)}
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
	if offset > 0 {
		bar.SetCurrent(offset)
	}

	reader := bar.ProxyReader(resp.Body)
	defer reader.Close()

	_, copyErr := io.Copy(outFile, reader)
	outFile.Close()

	if copyErr != nil {
		return Result{File: file, Err: fmt.Errorf("download failed: %w", copyErr)}
	}

	if err := os.Rename(tmpPath, localPath); err != nil {
		return Result{File: file, Err: fmt.Errorf("rename failed: %w", err)}
	}

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
