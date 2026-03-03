package main

import (
	"fmt"
	"os"

	flag "github.com/spf13/pflag"
	"onedrive-dl/dl"
	"onedrive-dl/od"
)

func main() {
	outDir := flag.StringP("output", "o", ".", "Download output directory")
	pwd := flag.StringP("password", "p", "", "Share password (if protected)")
	jobs := flag.IntP("jobs", "j", 3, "Concurrent file downloads")
	conns := flag.IntP("conn", "c", 8, "Connections per file")
	listMode := flag.BoolP("list", "l", false, "List files only, do not download")
	verbose := flag.Bool("verbose", false, "Verbose logging")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: onedrive-dl [options] <share-link-URL>\n\n")
		fmt.Fprintf(os.Stderr, "Download files from OneDrive/SharePoint share links.\n\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	shareURL := flag.Arg(0)

	if err := run(shareURL, *outDir, *pwd, *jobs, *conns, *listMode, *verbose); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(shareURL, outDir, pwd string, jobs, conns int, listMode, verbose bool) error {
	client := od.NewClient(verbose)

	fmt.Println("Resolving share link...")
	shareType, finalURL, body, err := od.Detect(client, shareURL)
	if err != nil {
		return fmt.Errorf("failed to detect share type: %w", err)
	}
	fmt.Printf("Detected: %s\n", shareType)

	if od.NeedPwd(finalURL, body) {
		if pwd == "" {
			return fmt.Errorf("this share is password-protected, use -p <password>")
		}
		pwHandler := &od.PwdHandler{Client: client}
		finalURL, body, err = pwHandler.Submit(finalURL, body, pwd, shareURL)
		if err != nil {
			return fmt.Errorf("password authentication failed: %w", err)
		}
		shareType, finalURL, body, err = od.Detect(client, finalURL)
		if err != nil {
			return fmt.Errorf("failed to detect share type after password: %w", err)
		}
		fmt.Printf("Detected: %s\n", shareType)
	}

	fmt.Println("Listing files...")
	var info *od.ShareInfo

	switch shareType {
	case od.TypeSP:
		handler := &od.SPHandler{Client: client}
		info, err = handler.ListFiles(finalURL, body)
	case od.TypePersonal:
		handler := &od.PersonalHandler{Client: client}
		info, err = handler.ListFiles(finalURL, body)
	default:
		return fmt.Errorf("unsupported share type")
	}
	if err != nil {
		return fmt.Errorf("failed to list files: %w", err)
	}

	if info.TotalFiles == 0 {
		fmt.Println("No files found in this share.")
		return nil
	}

	showTree(info)

	if listMode {
		return nil
	}

	d := dl.New(outDir, jobs, conns, verbose, client.HTTP.GetClient())
	results := d.Start(info)
	dl.Summary(results)

	for _, r := range results {
		if r.Err != nil {
			return fmt.Errorf("some downloads failed")
		}
	}
	return nil
}
