package api

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"time"

	"github.com/chromedp/chromedp"
)

// InitBrowser creates a persistent headless Chrome allocator context.
// It verifies Chrome is launchable and returns the context, or nil if
// Chrome is not available. Call the returned cancel func on shutdown.
func InitBrowser() (context.Context, context.CancelFunc) {
	// Log which Chrome binary chromedp will use.
	chromePath := findChromePath()
	if chromePath == "" {
		log.Printf("PDF export: no Chrome/Chromium found — PDF export disabled")
		log.Printf("PDF export: install Google Chrome or Chromium to enable")
		if runtime.GOOS == "darwin" {
			log.Printf("PDF export: expected at /Applications/Google Chrome.app or /Applications/Chromium.app")
		}
		return nil, func() {}
	}
	log.Printf("PDF export: using Chrome at %s", chromePath)

	// Verify Chrome can actually launch (macOS Gatekeeper may block it).
	if err := testChromeLaunch(chromePath); err != nil {
		log.Printf("PDF export: Chrome failed to launch: %v", err)
		if runtime.GOOS == "darwin" {
			log.Printf("PDF export: macOS may have blocked Chrome. Try:")
			log.Printf("PDF export:   xattr -cr '%s'", chromePath)
			log.Printf("PDF export:   or allow it in System Settings > Privacy & Security")
		}
		log.Printf("PDF export: PDF export disabled")
		return nil, func() {}
	}

	// Create a persistent allocator with headless Chrome.
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)

	// Warm up: launch browser once to verify it works and keep it hot.
	testCtx, testCancel := chromedp.NewContext(allocCtx)
	if err := chromedp.Run(testCtx, chromedp.Navigate("about:blank")); err != nil {
		log.Printf("PDF export: Chrome warm-up failed: %v", err)
		testCancel()
		allocCancel()
		return nil, func() {}
	}
	testCancel()

	log.Printf("PDF export: Chrome ready")
	return allocCtx, allocCancel
}

// findChromePath returns the path to Chrome/Chromium, or "" if not found.
func findChromePath() string {
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
		}
	default:
		candidates = []string{
			"chromium",
			"chromium-browser",
			"google-chrome",
			"google-chrome-stable",
		}
	}

	for _, c := range candidates {
		if path, err := exec.LookPath(c); err == nil {
			return path
		}
	}
	return ""
}

// testChromeLaunch tries to run Chrome --version to verify it's executable.
func testChromeLaunch(chromePath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, chromePath, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v (output: %s)", err, string(out))
	}
	log.Printf("PDF export: %s", string(out))
	return nil
}
