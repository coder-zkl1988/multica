package designpreview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image/color"
	"image/png"
	"math"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

const (
	browserTargetTimeout = 25 * time.Second
	browserSettleDelay   = 750 * time.Millisecond
	screenshotMaxSamples = 1_000_000
)

const domMetricsExpression = `(() => {
  const body = document.body;
  const root = document.documentElement;
  if (!body || !root) {
    return { documentLoaded: false, domPresent: false, computedVisibilityVisible: false, renderedElementCount: 0, visibleTextLength: 0, bodyWidth: 0, bodyHeight: 0, imageCount: 0, failedImageCount: 0 };
  }
  const bodyStyle = getComputedStyle(body);
  const opacity = Number.parseFloat(bodyStyle.opacity || '1');
  const visible = bodyStyle.display !== 'none' && bodyStyle.visibility !== 'hidden' && opacity > 0;
  const elements = Array.from(body.querySelectorAll('*'));
  let renderedElementCount = 0;
  const clips = (value) => value === 'hidden' || value === 'clip' || value === 'scroll' || value === 'auto';
  const maskMayPaint = (maskImage) => {
    const value = (maskImage || 'none').trim().toLowerCase();
    if (value === 'none') return true;
    if (value.includes('url(') || value.includes('image(') || value.includes('paint(')) return true;

    let onlyTransparentColorsSeen = value.includes('transparent');
    const parseAlpha = (rawAlpha) => {
      const trimmed = rawAlpha.trim();
      const alpha = trimmed.endsWith('%') ? Number.parseFloat(trimmed) / 100 : Number.parseFloat(trimmed);
      return Number.isFinite(alpha) ? alpha : null;
    };
    const colorFunctionAlpha = (color) => {
      const name = color.slice(0, color.indexOf('('));
      const body = color.slice(color.indexOf('(') + 1, -1);
      if (body.includes('/')) return parseAlpha(body.slice(body.lastIndexOf('/') + 1));
      if (name.endsWith('a')) {
        const parts = body.split(',');
        if (parts.length >= 4) return parseAlpha(parts[3]);
      }
      return null;
    };
    const hexColors = value.match(/#[0-9a-f]{3,8}\b/g) || [];
    for (const color of hexColors) {
      const hex = color.slice(1);
      if ((hex.length === 4 && hex[3] === '0') || (hex.length === 8 && hex.slice(6) === '00')) {
        onlyTransparentColorsSeen = true;
        continue;
      }
      return true;
    }

    const colorFunctions = value.match(/(?:rgb|hsl)a?\([^)]*\)/g) || [];
    for (const color of colorFunctions) {
      const alpha = colorFunctionAlpha(color);
      if (alpha !== null && alpha <= 0) {
        onlyTransparentColorsSeen = true;
        continue;
      }
      return true;
    }

    const namedOpaqueColors = /\b(?:black|white|red|green|blue|yellow|orange|purple|cyan|magenta|gray|grey|currentcolor)\b/.test(value.replace(/transparent/g, ''));
    if (namedOpaqueColors) return true;
    return !onlyTransparentColorsSeen;
  };
  const withPointerEventsEnabled = (element, action) => {
    const changed = [];
    for (let current = element; current; current = current.parentElement) {
      changed.push([current, current.getAttribute('style')]);
      current.style.setProperty('pointer-events', 'auto', 'important');
    }
    try {
      return action();
    } finally {
      for (const [node, value] of changed) {
        if (value === null) {
          node.removeAttribute('style');
        } else {
          node.setAttribute('style', value);
        }
      }
    }
  };
  const hasPaintHit = (element, left, top, right, bottom) => {
    const samples = [0.1, 0.25, 0.5, 0.75, 0.9];
    return withPointerEventsEnabled(element, () => {
      for (const xRatio of samples) {
        for (const yRatio of samples) {
          const x = left + (right - left) * xRatio;
          const y = top + (bottom - top) * yRatio;
          const hits = document.elementsFromPoint(x, y);
          if (hits.some((hit) => hit === element || element.contains(hit))) return true;
        }
      }
      return false;
    });
  };
  const isRendered = (element) => {
    const rect = element.getBoundingClientRect();
    if (rect.width <= 0.5 || rect.height <= 0.5) return false;
    let left = Math.max(rect.left, 0);
    let top = Math.max(rect.top, 0);
    let right = Math.min(rect.right, window.innerWidth);
    let bottom = Math.min(rect.bottom, window.innerHeight);
    let opacityProduct = 1;
    let requiresPaintHit = false;
    for (let current = element; current; current = current.parentElement) {
      const style = getComputedStyle(current);
      const currentOpacity = Number.parseFloat(style.opacity || '1');
      opacityProduct *= Number.isFinite(currentOpacity) ? currentOpacity : 1;
      if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse' || style.contentVisibility === 'hidden' || opacityProduct <= 0) return false;
      const maskImage = style.maskImage || style.webkitMaskImage || 'none';
      if (maskImage !== 'none') {
        if (!maskMayPaint(maskImage)) return false;
        requiresPaintHit = true;
      }
      if ((style.clipPath && style.clipPath !== 'none') || (style.clip && style.clip !== 'auto')) requiresPaintHit = true;
      if (current !== element && (clips(style.overflowX) || clips(style.overflowY))) {
        const clip = current.getBoundingClientRect();
        if (clips(style.overflowX)) {
          left = Math.max(left, clip.left);
          right = Math.min(right, clip.right);
        }
        if (clips(style.overflowY)) {
          top = Math.max(top, clip.top);
          bottom = Math.min(bottom, clip.bottom);
        }
      }
      if (right - left <= 0.5 || bottom - top <= 0.5) return false;
    }
    if (requiresPaintHit && !hasPaintHit(element, left, top, right, bottom)) return false;
    return true;
  };
  for (const element of elements) {
    if (isRendered(element)) {
      renderedElementCount += 1;
    }
  }
  const images = Array.from(body.querySelectorAll('img'));
  return {
    documentLoaded: document.readyState === 'interactive' || document.readyState === 'complete',
    domPresent: body.childNodes.length > 0,
    computedVisibilityVisible: visible,
    renderedElementCount,
    visibleTextLength: (body.innerText || '').trim().length,
    bodyWidth: Math.ceil(Math.max(body.scrollWidth, body.offsetWidth, root.clientWidth, root.scrollWidth, root.offsetWidth)),
    bodyHeight: Math.ceil(Math.max(body.scrollHeight, body.offsetHeight, root.clientHeight, root.scrollHeight, root.offsetHeight)),
    imageCount: images.length,
    failedImageCount: images.filter((image) => !image.complete || image.naturalWidth <= 0 || image.naturalHeight <= 0).length,
  };
})()`

const interactionStartExpression = `(() => {
  const body = document.body;
  if (!body) return { count: 0, before: 0 };
  const visible = (element) => {
    const style = getComputedStyle(element);
    const rect = element.getBoundingClientRect();
    return style.display !== 'none' && style.visibility !== 'hidden' && Number.parseFloat(style.opacity || '1') > 0 && rect.width > 0 && rect.height > 0 && !element.disabled;
  };
  const stateHash = () => {
    let hash = 2166136261;
    const controls = Array.from(body.querySelectorAll('input,select,textarea,details'));
    const value = body.innerHTML + '\u0000' + controls.map((element) => String(element.value || '') + ':' + String(element.checked || false) + ':' + String(element.open || false)).join('|');
    for (let index = 0; index < value.length; index += 1) {
      hash ^= value.charCodeAt(index);
      hash = Math.imul(hash, 16777619);
    }
    return hash >>> 0;
  };
  const elements = Array.from(body.querySelectorAll('button,input,select,textarea,summary,[role="button"]')).filter(visible).slice(0, 16);
  const before = stateHash();
  for (const element of elements) {
    if (element instanceof HTMLInputElement && !['button','submit','reset','checkbox','radio'].includes(element.type)) {
      element.value = element.value ? element.value + ' preview' : 'Preview';
      element.dispatchEvent(new Event('input', { bubbles: true }));
      element.dispatchEvent(new Event('change', { bubbles: true }));
    } else if (element instanceof HTMLTextAreaElement) {
      element.value = element.value ? element.value + ' preview' : 'Preview';
      element.dispatchEvent(new Event('input', { bubbles: true }));
      element.dispatchEvent(new Event('change', { bubbles: true }));
    } else if (element instanceof HTMLSelectElement && element.options.length > 1) {
      element.selectedIndex = element.selectedIndex === 0 ? 1 : 0;
      element.dispatchEvent(new Event('change', { bubbles: true }));
    } else {
      element.click();
    }
  }
  window.__multicaPreviewStateHash = stateHash;
  return { count: elements.length, before };
})()`

const interactionEndExpression = `(() => {
  const hash = window.__multicaPreviewStateHash;
  return typeof hash === 'function' ? hash() : 0;
})()`

type ChromiumVerifier struct {
	browserPath string
	policy      Policy
}

type domMetrics struct {
	DocumentLoaded            bool `json:"documentLoaded"`
	DOMPresent                bool `json:"domPresent"`
	ComputedVisibilityVisible bool `json:"computedVisibilityVisible"`
	RenderedElementCount      int  `json:"renderedElementCount"`
	VisibleTextLength         int  `json:"visibleTextLength"`
	BodyWidth                 int  `json:"bodyWidth"`
	BodyHeight                int  `json:"bodyHeight"`
	ImageCount                int  `json:"imageCount"`
	FailedImageCount          int  `json:"failedImageCount"`
}

type interactionStart struct {
	Count  int    `json:"count"`
	Before uint32 `json:"before"`
}

type networkEvidence struct {
	mu               sync.Mutex
	allowedOrigin    string
	failedRequests   map[network.RequestID]struct{}
	outboundRequests map[network.RequestID]struct{}
	requestURLs      map[network.RequestID]string
	consoleErrors    int
	interceptionErr  error
}

func ResolveBrowserPath(explicitPath string) (string, error) {
	if strings.TrimSpace(explicitPath) != "" {
		return validateBrowserPath(explicitPath)
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if candidate, err := exec.LookPath(name); err == nil {
			if browserPath, err := validateBrowserPath(candidate); err == nil {
				return browserPath, nil
			}
		}
	}
	candidates := []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/usr/bin/google-chrome",
		"/usr/bin/google-chrome-stable",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
	}
	for _, candidate := range candidates {
		if browserPath, err := validateBrowserPath(candidate); err == nil {
			return browserPath, nil
		}
	}
	return "", fmt.Errorf("no Chromium browser is installed on %s", runtime.GOOS)
}

func validateBrowserPath(rawBrowserPath string) (string, error) {
	browserPath, err := filepath.Abs(strings.TrimSpace(rawBrowserPath))
	if err != nil {
		return "", fmt.Errorf("resolve Design Preview browser path: %w", err)
	}
	browserPath, err = filepath.EvalSymlinks(browserPath)
	if err != nil {
		return "", fmt.Errorf("resolve Design Preview browser symlinks: %w", err)
	}
	info, err := os.Lstat(browserPath)
	if err != nil {
		return "", fmt.Errorf("inspect Design Preview browser: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", errors.New("Design Preview browser must be an executable regular file")
	}
	return browserPath, nil
}

func NewChromiumVerifier(rawBrowserPath string) (*ChromiumVerifier, error) {
	return NewChromiumVerifierWithPolicy(rawBrowserPath, DefaultPolicy())
}

func NewChromiumVerifierWithPolicy(rawBrowserPath string, policy Policy) (*ChromiumVerifier, error) {
	browserPath, err := ResolveBrowserPath(rawBrowserPath)
	if err != nil {
		return nil, err
	}
	if policy.ViewportWidth <= 0 || policy.ViewportHeight <= 0 || policy.MinEntropy < 0 || policy.MinMaxChannelStddev < 0 {
		return nil, errors.New("Design Preview browser policy is invalid")
	}
	return &ChromiumVerifier{browserPath: browserPath, policy: policy}, nil
}

func (v *ChromiumVerifier) Verify(ctx context.Context, targets []TargetURL) (Verification, error) {
	allowedOrigin, err := validateBrowserTargets(targets)
	if err != nil {
		return Verification{}, err
	}
	profileDir, err := os.MkdirTemp("", "multica-design-preview-")
	if err != nil {
		return Verification{}, fmt.Errorf("create isolated Design Preview browser profile: %w", err)
	}
	defer os.RemoveAll(profileDir)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(v.browserPath),
		chromedp.UserDataDir(profileDir),
		chromedp.WindowSize(v.policy.ViewportWidth, v.policy.ViewportHeight),
	)
	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAllocator()
	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	defer cancelBrowser()
	if err := chromedp.Run(browserCtx); err != nil {
		return Verification{}, fmt.Errorf("start isolated Design Preview browser: %w", err)
	}
	browserIdentity, err := readBrowserIdentity(browserCtx)
	if err != nil {
		return Verification{}, err
	}

	verification := Verification{
		Browser: browserIdentity,
		Policy:  v.policy,
		Targets: make([]TargetVerification, 0, len(targets)),
		Passed:  true,
	}
	for _, target := range targets {
		capture, err := v.captureTarget(browserCtx, allowedOrigin, target)
		if err != nil {
			return Verification{}, err
		}
		result := EvaluateCapture(capture, v.policy)
		verification.Targets = append(verification.Targets, result)
		verification.Passed = verification.Passed && result.Passed
	}
	return verification, nil
}

func (v *ChromiumVerifier) captureTarget(parent context.Context, allowedOrigin string, target TargetURL) (Capture, error) {
	targetCtx, cancelTarget := chromedp.NewContext(parent)
	defer cancelTarget()
	targetCtx, cancelTimeout := context.WithTimeout(targetCtx, browserTargetTimeout)
	defer cancelTimeout()

	evidence := &networkEvidence{
		allowedOrigin:    allowedOrigin,
		failedRequests:   make(map[network.RequestID]struct{}),
		outboundRequests: make(map[network.RequestID]struct{}),
		requestURLs:      make(map[network.RequestID]string),
	}
	evidence.listen(targetCtx)
	if err := chromedp.Run(targetCtx,
		network.Enable(),
		network.SetCacheDisabled(true),
		network.SetBypassServiceWorker(true),
		fetch.Enable(),
		chromedp.EmulateViewport(int64(v.policy.ViewportWidth), int64(v.policy.ViewportHeight)),
	); err != nil {
		return Capture{}, fmt.Errorf("prepare Design Preview browser target: %w", err)
	}

	capture := Capture{Target: target.Target}
	if err := chromedp.Run(targetCtx, chromedp.Navigate(target.URL)); err != nil {
		if interceptionErr := evidence.interceptionError(); interceptionErr != nil {
			return Capture{}, fmt.Errorf("intercept Design Preview requests for %q: %w", target.Target.Path, interceptionErr)
		}
		capture.FailedResourceCount, capture.ConsoleErrorCount, capture.OutboundRequestCount = evidence.counts()
		return capture, nil
	}
	if err := chromedp.Run(targetCtx, chromedp.Sleep(browserSettleDelay)); err != nil {
		capture.FailedResourceCount, capture.ConsoleErrorCount, capture.OutboundRequestCount = evidence.counts()
		return capture, nil
	}

	interaction := interactionStart{}
	interactionAfter := uint32(0)
	if target.RequireInteraction {
		if err := chromedp.Run(targetCtx,
			chromedp.Evaluate(interactionStartExpression, &interaction),
			chromedp.Sleep(100*time.Millisecond),
			chromedp.Evaluate(interactionEndExpression, &interactionAfter),
		); err != nil {
			return Capture{}, fmt.Errorf("exercise Design Preview interactions for %q: %w", target.Target.Path, err)
		}
	}

	var dom domMetrics
	var screenshot []byte
	if err := chromedp.Run(targetCtx,
		chromedp.Evaluate(domMetricsExpression, &dom),
		page.BringToFront(),
		chromedp.CaptureScreenshot(&screenshot),
	); err != nil {
		return Capture{}, fmt.Errorf("capture Design Preview evidence for %q: %w", target.Target.Path, err)
	}
	dom.BodyWidth = min(dom.BodyWidth, dimensionEvidenceMax)
	dom.BodyHeight = min(dom.BodyHeight, dimensionEvidenceMax)
	screenshotMetrics, err := analyzeScreenshot(screenshot)
	if err != nil {
		return Capture{}, fmt.Errorf("analyze Design Preview screenshot for %q: %w", target.Target.Path, err)
	}
	failedResources, consoleErrors, outboundRequests := evidence.counts()
	return Capture{
		Target:                    target.Target,
		DocumentLoaded:            dom.DocumentLoaded,
		DOMPresent:                dom.DOMPresent,
		ComputedVisibilityVisible: dom.ComputedVisibilityVisible,
		RenderedElementCount:      dom.RenderedElementCount,
		VisibleTextLength:         dom.VisibleTextLength,
		BodyWidth:                 dom.BodyWidth,
		BodyHeight:                dom.BodyHeight,
		ImageCount:                dom.ImageCount,
		FailedImageCount:          dom.FailedImageCount,
		FailedResourceCount:       failedResources,
		ConsoleErrorCount:         consoleErrors,
		OutboundRequestCount:      outboundRequests,
		InteractionRequired:       target.RequireInteraction,
		InteractiveElementCount:   interaction.Count,
		InteractionChanged:        target.RequireInteraction && interaction.Before != interactionAfter,
		Screenshot:                screenshotMetrics,
	}, nil
}

func (e *networkEvidence) listen(ctx context.Context) {
	chromedp.ListenTarget(ctx, func(event any) {
		switch event := event.(type) {
		case *fetch.EventRequestPaused:
			allowed := sameOrigin(event.Request.URL, e.allowedOrigin)
			requestID := event.RequestID
			go func() {
				chromedpContext := chromedp.FromContext(ctx)
				if chromedpContext == nil || chromedpContext.Target == nil {
					e.recordInterceptionError(errors.New("Design Preview target executor is unavailable"))
					return
				}
				executorCtx := cdp.WithExecutor(ctx, chromedpContext.Target)
				var err error
				if allowed {
					err = fetch.ContinueRequest(requestID).Do(executorCtx)
				} else {
					err = fetch.FailRequest(requestID, network.ErrorReasonBlockedByClient).Do(executorCtx)
				}
				if err != nil && !errors.Is(err, context.Canceled) {
					e.recordInterceptionError(err)
				}
			}()
		case *network.EventRequestWillBeSent:
			e.mu.Lock()
			e.requestURLs[event.RequestID] = event.Request.URL
			if !sameOrigin(event.Request.URL, e.allowedOrigin) {
				e.outboundRequests[event.RequestID] = struct{}{}
			}
			e.mu.Unlock()
		case *network.EventResponseReceived:
			if event.Response.Status >= 400 && !ignorableBrowserResource(event.Response.URL) {
				e.recordFailedRequest(event.RequestID)
			}
		case *network.EventLoadingFailed:
			e.mu.Lock()
			requestURL := e.requestURLs[event.RequestID]
			if _, outbound := e.outboundRequests[event.RequestID]; !outbound && !ignorableBrowserResource(requestURL) {
				e.failedRequests[event.RequestID] = struct{}{}
			}
			e.mu.Unlock()
		case *cdpruntime.EventConsoleAPICalled:
			if event.Type == cdpruntime.APITypeError {
				e.mu.Lock()
				e.consoleErrors++
				e.mu.Unlock()
			}
		case *cdpruntime.EventExceptionThrown:
			e.mu.Lock()
			e.consoleErrors++
			e.mu.Unlock()
		}
	})
}

func (e *networkEvidence) recordInterceptionError(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.interceptionErr == nil {
		e.interceptionErr = err
	}
}

func (e *networkEvidence) interceptionError() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.interceptionErr
}

func (e *networkEvidence) recordFailedRequest(requestID network.RequestID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, outbound := e.outboundRequests[requestID]; !outbound {
		e.failedRequests[requestID] = struct{}{}
	}
}

func (e *networkEvidence) counts() (failedResources, consoleErrors, outboundRequests int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.failedRequests), e.consoleErrors, len(e.outboundRequests)
}

func validateBrowserTargets(targets []TargetURL) (string, error) {
	if len(targets) == 0 || len(targets) > targetMaxCount {
		return "", errors.New("Design Preview browser has an invalid target count")
	}
	allowedOrigin := ""
	for _, target := range targets {
		if err := ValidateTarget(target.Target); err != nil {
			return "", err
		}
		parsed, err := url.Parse(target.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
			return "", errors.New("Design Preview browser target URL is invalid")
		}
		hostname := parsed.Hostname()
		address := net.ParseIP(hostname)
		if hostname != "localhost" && (address == nil || !address.IsLoopback()) {
			return "", errors.New("Design Preview browser target must use a loopback host")
		}
		origin := parsed.Scheme + "://" + parsed.Host
		if allowedOrigin == "" {
			allowedOrigin = origin
		} else if origin != allowedOrigin {
			return "", errors.New("Design Preview browser targets must use one origin")
		}
	}
	return allowedOrigin, nil
}

func sameOrigin(rawURL, allowedOrigin string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	switch parsed.Scheme {
	case "data", "blob", "about":
		return true
	case "http", "https":
		return parsed.Scheme+"://"+parsed.Host == allowedOrigin
	default:
		return false
	}
}

func ignorableBrowserResource(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && parsed.Path == "/favicon.ico"
}

func readBrowserIdentity(ctx context.Context) (BrowserIdentity, error) {
	chromedpContext := chromedp.FromContext(ctx)
	if chromedpContext == nil || chromedpContext.Browser == nil {
		return BrowserIdentity{}, errors.New("Design Preview browser context is unavailable")
	}
	_, product, _, _, _, err := browser.GetVersion().Do(cdp.WithExecutor(ctx, chromedpContext.Browser))
	if err != nil {
		return BrowserIdentity{}, fmt.Errorf("read Design Preview browser identity: %w", err)
	}
	name, version, found := strings.Cut(strings.TrimSpace(product), "/")
	if !found || name == "" || version == "" {
		return BrowserIdentity{}, errors.New("Design Preview browser returned an invalid identity")
	}
	return BrowserIdentity{Name: name, Version: version}, nil
}

func analyzeScreenshot(data []byte) (Screenshot, error) {
	if len(data) == 0 || len(data) > screenshotMaxBytes {
		return Screenshot{}, errors.New("Design Preview screenshot has an invalid size")
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return Screenshot{}, fmt.Errorf("decode Design Preview screenshot: %w", err)
	}
	bounds := decoded.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 || width > dimensionMax || height > dimensionMax {
		return Screenshot{}, errors.New("Design Preview screenshot has invalid dimensions")
	}

	pixelCount := width * height
	stride := 1
	if pixelCount > screenshotMaxSamples {
		stride = int(math.Ceil(float64(pixelCount) / screenshotMaxSamples))
	}
	var histogram [256]uint64
	var sums, squareSums [3]float64
	samples := 0
	index := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if index%stride != 0 {
				index++
				continue
			}
			index++
			pixel := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
			channels := [3]float64{float64(pixel.R), float64(pixel.G), float64(pixel.B)}
			for channel := range channels {
				sums[channel] += channels[channel]
				squareSums[channel] += channels[channel] * channels[channel]
			}
			gray := (299*uint64(pixel.R) + 587*uint64(pixel.G) + 114*uint64(pixel.B)) / 1000
			histogram[gray]++
			samples++
		}
	}
	if samples == 0 {
		return Screenshot{}, errors.New("Design Preview screenshot has no pixels")
	}

	entropy := 0.0
	for _, count := range histogram {
		if count == 0 {
			continue
		}
		probability := float64(count) / float64(samples)
		entropy -= probability * math.Log2(probability)
	}
	maxStddev := 0.0
	for channel := range sums {
		mean := sums[channel] / float64(samples)
		variance := squareSums[channel]/float64(samples) - mean*mean
		if variance < 0 {
			variance = 0
		}
		maxStddev = math.Max(maxStddev, math.Sqrt(variance))
	}
	digest := sha256.Sum256(data)
	return Screenshot{
		SHA256:           "sha256:" + hex.EncodeToString(digest[:]),
		Bytes:            len(data),
		Width:            width,
		Height:           height,
		Entropy:          entropy,
		MaxChannelStddev: maxStddev,
	}, nil
}
