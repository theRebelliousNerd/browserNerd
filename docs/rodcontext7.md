# Rod - Go Browser Automation Library

Rod is a high-level web automation and scraping library for Go that directly controls browsers via the DevTools Protocol. It provides a simple, chainable API for automating Chrome/Chromium browsers with support for both headless and headed modes. Rod automatically finds or downloads browsers, handles nested iframes and shadow DOMs, and maintains thread-safe operations across all functions. The library emphasizes reliability with 100% test coverage and intelligent features like auto-waiting for elements to be ready, never missing events with its two-step event design, and preventing zombie browser processes after crashes.

Rod excels at web scraping, browser testing, and automated workflows requiring precise browser control. It offers both high-level convenience methods for common tasks and low-level CDP access for advanced customization. The context-based timeout design integrates seamlessly with Go's standard patterns, while features like request hijacking, file downloads, screenshot capture, and PDF generation make it suitable for a wide range of automation scenarios. The library supports concurrent operations through browser and page pools, making it efficient for large-scale automation tasks.

## Browser Initialization and Connection

Create and connect to a browser instance with automatic launcher support.

```go
package main

import (
    "github.com/go-rod/rod"
    "github.com/go-rod/rod/lib/launcher"
)

func main() {
    // Basic connection - automatically finds or downloads browser
    browser := rod.New().MustConnect()
    defer browser.MustClose()

    // Custom browser launch with specific flags
    url := launcher.New().
        Headless(false).  // Run with UI visible
        Devtools(true).   // Open DevTools automatically
        MustLaunch()

    customBrowser := rod.New().
        ControlURL(url).
        SlowMotion(2 * time.Second).  // Add delay between actions
        Trace(true).                   // Enable action tracing
        MustConnect()
    defer customBrowser.MustClose()

    // Create incognito browser context
    incognito, err := browser.Incognito()
    if err != nil {
        panic(err)
    }
    defer incognito.MustClose()
}
```

## Page Navigation and Basic Operations

Navigate to URLs and perform basic page interactions.

```go
package main

import (
    "fmt"
    "github.com/go-rod/rod"
)

func main() {
    browser := rod.New().MustConnect()
    defer browser.MustClose()

    // Create page and navigate
    page := browser.MustPage("https://github.com")
    defer page.MustClose()

    // Wait for page to be stable (no network activity or DOM changes)
    page.MustWaitStable(time.Second)

    // Get page information
    info, err := page.Info()
    if err != nil {
        panic(err)
    }
    fmt.Printf("Title: %s\n", info.Title)
    fmt.Printf("URL: %s\n", info.URL)

    // Navigate with proper error handling
    err = page.Navigate("https://example.com")
    if err != nil {
        if navErr, ok := err.(*rod.NavigationError); ok {
            fmt.Printf("Navigation failed: %s\n", navErr.Error())
        }
        panic(err)
    }

    // Reload, back, forward navigation
    page.MustReload()
    page.MustNavigateBack()
    page.MustNavigateForward()

    // Get page HTML
    html, err := page.HTML()
    if err != nil {
        panic(err)
    }
    fmt.Println(html)
}
```

## Element Selection and Interaction

Find elements using CSS selectors, XPath, or regex and interact with them.

```go
package main

import (
    "fmt"
    "github.com/go-rod/rod"
    "github.com/go-rod/rod/lib/input"
)

func main() {
    page := rod.New().MustConnect().MustPage("https://github.com")
    defer page.MustClose()

    // CSS selector - auto-retries until element appears
    searchInput := page.MustElement("input[name='q']")

    // XPath selector
    submitButton := page.MustElementX("//button[@type='submit']")

    // Regex selector - finds element by text content
    heading := page.MustElementR("h1", "GitHub")

    // Check if element exists without retrying
    has, element, err := page.Has("div.header")
    if err != nil {
        panic(err)
    }
    if has {
        fmt.Println("Header found:", element)
    }

    // Get all matching elements
    links := page.MustElements("a")
    fmt.Printf("Found %d links\n", len(links))

    // Element interactions
    searchInput.MustClick()
    searchInput.MustInput("go-rod/rod")
    searchInput.MustType(input.Enter)

    // Get element properties and attributes
    text := heading.MustText()
    href, _ := element.Attribute("href")
    visible, _ := element.Visible()

    fmt.Printf("Text: %s, Href: %s, Visible: %t\n", text, href, visible)

    // Wait for element states
    button := page.MustElement("button")
    button.MustWaitVisible()
    button.MustWaitEnabled()
    button.MustWaitInteractable()
    button.MustClick()
}
```

## JavaScript Evaluation

Execute JavaScript code in the page context.

```go
package main

import (
    "fmt"
    "github.com/go-rod/rod"
)

func main() {
    page := rod.New().MustConnect().MustPage("https://example.com")
    defer page.MustClose()

    // Simple evaluation with parameters
    result := page.MustEval(`(a, b) => a + b`, 10, 20)
    fmt.Printf("10 + 20 = %d\n", result.Int())

    // Get page properties
    title := page.MustEval(`() => document.title`)
    fmt.Printf("Title: %s\n", title.Str())

    // Evaluate on element - 'this' refers to the element
    element := page.MustElement("h1")
    innerHTML := element.MustEval(`() => this.innerHTML`)
    fmt.Println("H1 content:", innerHTML.Str())

    // Async evaluation with promises
    asyncResult := page.MustEval(`async () => {
        await new Promise(resolve => setTimeout(resolve, 1000))
        return "done"
    }`)
    fmt.Println("Async result:", asyncResult.Str())

    // Share remote object between evaluations
    mathRandom := page.MustEvaluate(rod.Eval(`() => Math.random`).ByObject())
    randomNum := page.MustEval(`f => f()`, mathRandom)
    fmt.Printf("Random number: %f\n", randomNum.Num())

    // Add custom scripts to page
    page.MustAddScriptTag("", `
        window.customFunction = function() {
            return "Hello from injected script"
        }
    `)

    customResult := page.MustEval(`() => window.customFunction()`)
    fmt.Println(customResult.Str())
}
```

## Waiting Strategies and Timeouts

Control timing and wait for various conditions.

```go
package main

import (
    "context"
    "fmt"
    "time"
    "github.com/go-rod/rod"
    "github.com/go-rod/rod/lib/proto"
)

func main() {
    browser := rod.New().MustConnect()
    defer browser.MustClose()

    page := browser.MustPage("https://example.com")
    defer page.MustClose()

    // Set timeout for operations
    page.Timeout(5 * time.Second).MustElement("button").CancelTimeout()

    // Context-based cancellation
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    pageWithCtx := page.Context(ctx)
    element, err := pageWithCtx.Element("input")
    if err == context.DeadlineExceeded {
        fmt.Println("Operation timed out")
    }

    // Wait for page load
    page.MustWaitLoad()

    // Wait for navigation events
    waitNav := page.WaitNavigation(proto.PageLifecycleEventNameNetworkAlmostIdle)
    page.MustElement("a").MustClick()
    waitNav()

    // Wait for network to be idle (no requests for specified duration)
    wait := page.WaitRequestIdle(time.Second, []string{"api"}, nil, nil)
    page.MustElement("button").MustClick()
    wait()

    // Wait for DOM to be stable (minimal changes)
    err = page.WaitDOMStable(300*time.Millisecond, 0.0)
    if err != nil {
        panic(err)
    }

    // Wait for element to be stable (no position/size changes)
    element.MustWaitStable(500 * time.Millisecond)

    // Wait for custom JavaScript condition
    err = page.Wait(rod.Eval(`() => document.querySelectorAll('li').length > 10`))
    if err != nil {
        panic(err)
    }

    // Wait for animation frame
    page.MustWaitRepaint()

    // Wait until specific number of elements exist
    page.MustWaitElementsMoreThan("div.item", 5)
}
```

## Event Handling and Monitoring

Listen to browser and page events.

```go
package main

import (
    "fmt"
    "github.com/go-rod/rod"
    "github.com/go-rod/rod/lib/proto"
)

func main() {
    browser := rod.New().MustConnect()
    defer browser.MustClose()

    page := browser.MustPage()
    defer page.MustClose()

    // Listen for console logs
    go page.EachEvent(func(e *proto.RuntimeConsoleAPICalled) {
        if e.Type == proto.RuntimeConsoleAPICalledTypeLog {
            fmt.Println("Console:", page.MustObjectsToJSON(e.Args))
        }
    })()

    // Wait for single event
    loadEvent := &proto.PageLoadEventFired{}
    wait := page.WaitEvent(loadEvent)
    page.MustNavigate("https://example.com")
    wait()
    fmt.Println("Page loaded")

    // Handle multiple event types
    wait = page.EachEvent(
        func(e *proto.NetworkRequestWillBeSent) {
            fmt.Println("Request:", e.Request.URL)
        },
        func(e *proto.NetworkResponseReceived) {
            fmt.Println("Response:", e.Response.Status)
        },
    )

    page.MustNavigate("https://github.com")
    wait()

    // Wait for new page to open
    waitOpen := page.WaitOpen()
    page.MustElement("a[target='_blank']").MustClick()
    newPage, err := waitOpen()
    if err == nil {
        fmt.Println("New page opened:", newPage.MustInfo().URL)
        newPage.MustClose()
    }

    // Browser-level events
    go browser.EachEvent(func(e *proto.TargetTargetCreated) {
        fmt.Println("New target created:", e.TargetInfo.URL)
    })()
}
```

## Request and Response Hijacking

Intercept and modify network requests and responses.

```go
package main

import (
    "fmt"
    "net/http"
    "github.com/go-rod/rod"
    "github.com/go-rod/rod/lib/proto"
)

func main() {
    browser := rod.New().MustConnect()
    defer browser.MustClose()

    // Create hijack router
    router := browser.HijackRequests()
    defer router.MustStop()

    // Intercept JavaScript files
    router.MustAdd("*.js", proto.NetworkResourceTypeScript, func(ctx *rod.Hijack) {
        // Modify request headers
        ctx.Request.Req().Header.Set("Custom-Header", "my-value")

        // Load actual response from server
        err := ctx.LoadResponse(http.DefaultClient, true)
        if err != nil {
            ctx.OnError(err)
            return
        }

        // Modify response body
        originalBody := ctx.Response.Body()
        modifiedBody := originalBody + "\nconsole.log('Injected by Rod');"
        ctx.Response.SetBody(modifiedBody)

        // Modify response headers
        ctx.Response.SetHeader("X-Modified", "true")
    })

    // Block specific requests
    router.MustAdd("*.png", proto.NetworkResourceTypeImage, func(ctx *rod.Hijack) {
        ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient)
    })

    // Mock API responses
    router.MustAdd("*/api/*", proto.NetworkResourceTypeXHR, func(ctx *rod.Hijack) {
        mockData := map[string]interface{}{
            "status": "success",
            "data":   []string{"item1", "item2"},
        }

        ctx.Response.SetBody(mockData)
        ctx.Response.SetHeader("Content-Type", "application/json")
        ctx.Response.Payload().ResponseCode = 200
    })

    // Start the router
    go router.Run()

    page := browser.MustPage("https://example.com")
    defer page.MustClose()

    fmt.Println("Requests are being hijacked")
}
```

## Screenshots and PDF Generation

Capture page visuals in various formats.

```go
package main

import (
    "github.com/go-rod/rod"
    "github.com/go-rod/rod/lib/proto"
    "github.com/go-rod/rod/lib/utils"
    "github.com/ysmood/gson"
)

func main() {
    page := rod.New().MustConnect().MustPage("https://github.com")
    defer page.MustClose()

    page.MustWaitLoad()

    // Simple full-page screenshot
    page.MustScreenshot("fullpage.png")

    // Element screenshot
    element := page.MustElement("header")
    element.MustScreenshot(proto.PageCaptureScreenshotFormatJpeg, 90)

    // Custom screenshot with clipping
    data, err := page.Screenshot(false, &proto.PageCaptureScreenshot{
        Format:  proto.PageCaptureScreenshotFormatJpeg,
        Quality: gson.Int(90),
        Clip: &proto.PageViewport{
            X:      0,
            Y:      0,
            Width:  800,
            Height: 600,
            Scale:  1,
        },
    })
    if err != nil {
        panic(err)
    }
    utils.OutputFile("custom.jpg", data)

    // Scroll screenshot - captures entire scrollable page
    scrollData, err := page.ScrollScreenshot(&rod.ScrollScreenshotOptions{
        Format:        proto.PageCaptureScreenshotFormatPng,
        FixedTop:      60,  // Skip fixed header
        FixedBottom:   0,
        WaitPerScroll: 300 * time.Millisecond,
    })
    if err != nil {
        panic(err)
    }
    utils.OutputFile("scroll.png", scrollData)

    // Generate PDF
    page.MustPDF("output.pdf")

    // Custom PDF settings
    pdfStream, err := page.PDF(&proto.PagePrintToPDF{
        PaperWidth:              gson.Num(8.5),
        PaperHeight:             gson.Num(11),
        PrintBackground:         true,
        PreferCSSPageSize:       false,
        MarginTop:               gson.Num(0.4),
        MarginBottom:            gson.Num(0.4),
        MarginLeft:              gson.Num(0.4),
        MarginRight:             gson.Num(0.4),
        PageRanges:              "1-5",
        DisplayHeaderFooter:     true,
        HeaderTemplate:          "<div style='font-size:10px'>Header</div>",
        FooterTemplate:          "<div style='font-size:10px'>Page <span class='pageNumber'></span></div>",
        Scale:                   gson.Num(1),
    })
    if err != nil {
        panic(err)
    }

    pdfData := pdfStream.MustRead()
    utils.OutputFile("custom.pdf", pdfData)
}
```

## Form Handling and User Input

Simulate user interactions with forms and inputs.

```go
package main

import (
    "time"
    "github.com/go-rod/rod"
    "github.com/go-rod/rod/lib/input"
)

func main() {
    page := rod.New().MustConnect().MustPage("https://example.com/form")
    defer page.MustClose()

    // Text input
    nameInput := page.MustElement("input[name='username']")
    nameInput.MustClick()
    nameInput.MustInput("john_doe")

    // Select all and replace
    emailInput := page.MustElement("input[name='email']")
    emailInput.MustSelectAllText()
    emailInput.MustInput("new@example.com")

    // Select specific text with regex
    textArea := page.MustElement("textarea")
    textArea.MustSelectText("\\w+@\\w+\\.com")
    textArea.MustInput("replacement@test.com")

    // Dropdown selection
    dropdown := page.MustElement("select[name='country']")
    dropdown.MustSelect([]string{"USA"}, true, rod.SelectorTypeText)

    // Checkbox and radio buttons
    checkbox := page.MustElement("input[type='checkbox']")
    checkbox.MustClick()

    // File upload
    fileInput := page.MustElement("input[type='file']")
    fileInput.MustSetFiles([]string{
        "/path/to/document.pdf",
        "/path/to/image.png",
    })

    // Date/time input
    dateInput := page.MustElement("input[type='date']")
    dateInput.MustInputTime(time.Now())

    // Color picker
    colorInput := page.MustElement("input[type='color']")
    colorInput.MustInputColor("#ff0000")

    // Keyboard shortcuts
    page.MustElement("textarea").MustClick()
    page.KeyActions().
        Press(input.ControlLeft).
        Type(input.KeyA).      // Select all (Ctrl+A)
        Release(input.ControlLeft).
        Type(input.Delete).    // Delete selected
        MustDo()

    // Submit form
    submitBtn := page.MustElement("button[type='submit']")
    submitBtn.MustWaitEnabled()
    submitBtn.MustClick()

    // Handle dialogs
    go func() {
        wait, handle := page.MustHandleDialog()
        dialog := wait()
        fmt.Println("Alert text:", dialog.Message)
        handle(&proto.PageHandleJavaScriptDialog{Accept: true, PromptText: "response"})
    }()
}
```

## Cookies and Storage Management

Manage browser cookies and session data.

```go
package main

import (
    "fmt"
    "time"
    "github.com/go-rod/rod"
    "github.com/go-rod/rod/lib/proto"
)

func main() {
    browser := rod.New().MustConnect()
    defer browser.MustClose()

    page := browser.MustPage("https://example.com")
    defer page.MustClose()

    // Get page cookies
    cookies, err := page.Cookies([]string{"https://example.com"})
    if err != nil {
        panic(err)
    }

    for _, cookie := range cookies {
        fmt.Printf("%s = %s\n", cookie.Name, cookie.Value)
    }

    // Set cookies for page
    err = page.SetCookies([]*proto.NetworkCookieParam{
        {
            Name:     "session_id",
            Value:    "abc123xyz",
            Domain:   "example.com",
            Path:     "/",
            Secure:   true,
            HTTPOnly: true,
            SameSite: proto.NetworkCookieSameSiteStrict,
            Expires:  proto.TimeSinceEpoch(time.Now().Add(24 * time.Hour).Unix()),
        },
        {
            Name:   "user_pref",
            Value:  "dark_mode",
            Domain: "example.com",
            Path:   "/",
        },
    })
    if err != nil {
        panic(err)
    }

    // Clear page cookies
    page.MustSetCookies(nil)

    // Browser-level cookie management
    browserCookies, err := browser.GetCookies()
    if err != nil {
        panic(err)
    }
    fmt.Printf("Browser has %d cookies\n", len(browserCookies))

    // Set browser cookies
    browser.MustSetCookies([]*proto.NetworkCookieParam{
        {
            Name:   "global_setting",
            Value:  "enabled",
            Domain: ".example.com",
            Path:   "/",
        },
    })

    // Clear all browser cookies
    browser.MustSetCookies(nil)
}
```

## Advanced Element Queries and Racing

Search and race multiple selectors for dynamic content.

```go
package main

import (
    "fmt"
    "github.com/go-rod/rod"
)

func main() {
    page := rod.New().MustConnect().MustPage("https://example.com/search")
    defer page.MustClose()

    // Search across iframes and shadow DOM
    searchResult, err := page.Search("login button")
    if err != nil {
        panic(err)
    }
    defer searchResult.Release()

    fmt.Printf("Found %d matches\n", searchResult.ResultCount)
    firstElement := searchResult.First
    firstElement.MustClick()

    // Get multiple results from search
    elements, err := searchResult.Get(0, 5)
    if err != nil {
        panic(err)
    }
    for i, el := range elements {
        fmt.Printf("Element %d: %s\n", i, el.MustText())
    }

    // Race multiple selectors - first one found wins
    page.MustNavigate("https://example.com/login")

    element := page.Race().
        Element(".success-message").MustHandle(func(e *rod.Element) {
            fmt.Println("Login succeeded:", e.MustText())
        }).
        Element(".error-message").MustHandle(func(e *rod.Element) {
            fmt.Println("Login failed:", e.MustText())
        }).
        ElementR("button", "Try Again").MustHandle(func(e *rod.Element) {
            fmt.Println("Retry option appeared")
            e.MustClick()
        }).
        MustDo()

    // Check which selector matched
    isError, err := element.Matches(".error-message")
    if err != nil {
        panic(err)
    }
    if isError {
        fmt.Println("Error state detected")
    }

    // Custom race function
    page.Race().ElementFunc(func(p *rod.Page) (*rod.Element, error) {
        // Custom logic to find element
        elements := p.MustElements("div.item")
        if len(elements) > 10 {
            return elements[0], nil
        }
        return nil, &rod.ElementNotFoundError{}
    }).MustDo()
}
```

## Page and Browser Pools for Concurrency

Manage concurrent browser automation with resource pooling.

```go
package main

import (
    "fmt"
    "sync"
    "github.com/go-rod/rod"
)

func main() {
    // Browser pool for concurrent browser instances
    browserPool := rod.NewBrowserPool(3)
    defer browserPool.Cleanup(func(b *rod.Browser) { b.MustClose() })

    createBrowser := func() *rod.Browser {
        return rod.New().MustConnect()
    }

    var wg sync.WaitGroup
    urls := []string{
        "https://example.com",
        "https://github.com",
        "https://golang.org",
    }

    for _, url := range urls {
        wg.Add(1)
        go func(targetURL string) {
            defer wg.Done()

            browser := browserPool.MustGet(createBrowser)
            defer browserPool.Put(browser)

            page := browser.MustPage(targetURL)
            defer page.MustClose()

            title := page.MustInfo().Title
            fmt.Printf("Title of %s: %s\n", targetURL, title)
        }(url)
    }

    wg.Wait()

    // Page pool for concurrent page instances in same browser
    browser := rod.New().MustConnect()
    defer browser.MustClose()

    pagePool := rod.NewPagePool(5)
    defer pagePool.Cleanup(func(p *rod.Page) { p.MustClose() })

    createPage := func() *rod.Page {
        return browser.MustIncognito().MustPage()
    }

    tasks := make([]string, 10)
    for i := range tasks {
        tasks[i] = fmt.Sprintf("https://example.com/page%d", i)
    }

    for _, task := range tasks {
        wg.Add(1)
        go func(url string) {
            defer wg.Done()

            page := pagePool.MustGet(createPage)
            defer pagePool.Put(page)

            page.MustNavigate(url).MustWaitLoad()
            html, _ := page.HTML()
            fmt.Printf("Page %s loaded, size: %d bytes\n", url, len(html))
        }(task)
    }

    wg.Wait()
}
```

## File Downloads and Resource Management

Handle file downloads and page resources.

```go
package main

import (
    "fmt"
    "path/filepath"
    "github.com/go-rod/rod"
    "github.com/go-rod/rod/lib/utils"
)

func main() {
    browser := rod.New().MustConnect()
    defer browser.MustClose()

    page := browser.MustPage("https://example.com/downloads")
    defer page.MustClose()

    // Download file
    downloadDir := "/tmp/downloads"
    wait := browser.WaitDownload(downloadDir)

    page.MustElement("a.download-link").MustClick()

    info := wait()
    downloadedFile := filepath.Join(downloadDir, info.GUID)
    fmt.Printf("Downloaded: %s\n", downloadedFile)

    // Get resource content (images, CSS, JS)
    resourceTree, err := proto.PageGetResourceTree{}.Call(page)
    if err != nil {
        panic(err)
    }

    for _, resource := range resourceTree.FrameTree.Resources {
        fmt.Printf("Resource: %s (%s)\n", resource.URL, resource.Type)

        content, err := page.GetResource(resource.URL)
        if err != nil {
            fmt.Printf("Failed to get resource: %v\n", err)
            continue
        }

        filename := filepath.Base(resource.URL)
        utils.OutputFile(filename, content)
    }

    // Get image from element
    img := page.MustElement("img.logo")
    imgData, err := img.Resource()
    if err != nil {
        panic(err)
    }
    utils.OutputFile("logo.png", imgData)

    // Get background image
    div := page.MustElement("div.hero")
    bgImage, err := div.BackgroundImage()
    if err != nil {
        panic(err)
    }
    utils.OutputFile("background.jpg", bgImage)

    // Canvas to image
    canvas := page.MustElement("canvas")
    canvasData, err := canvas.CanvasToImage("image/png", 1.0)
    if err != nil {
        panic(err)
    }
    utils.OutputFile("canvas.png", canvasData)
}
```

## Direct CDP Protocol Access

Use Chrome DevTools Protocol directly for advanced features.

```go
package main

import (
    "context"
    "fmt"
    "github.com/go-rod/rod"
    "github.com/go-rod/rod/lib/proto"
)

func main() {
    browser := rod.New().MustConnect()
    defer browser.MustClose()

    page := browser.MustPage("https://example.com")
    defer page.MustClose()

    // Call CDP methods directly
    err := proto.PageSetAdBlockingEnabled{
        Enabled: true,
    }.Call(page)
    if err != nil {
        panic(err)
    }

    // Get performance metrics
    metrics, err := proto.PerformanceGetMetrics{}.Call(page)
    if err != nil {
        panic(err)
    }
    for _, metric := range metrics.Metrics {
        fmt.Printf("%s: %f\n", metric.Name, metric.Value)
    }

    // Enable domains for events
    restore := page.EnableDomain(&proto.NetworkEnable{})
    defer restore()

    // Low-level CDP JSON API call
    result, err := page.Call(
        context.TODO(),
        "",
        "Network.setCacheDisabled",
        map[string]bool{"cacheDisabled": true},
    )
    if err != nil {
        panic(err)
    }
    fmt.Println("CDP result:", string(result))

    // DOM snapshot for analysis
    snapshot, err := page.CaptureDOMSnapshot()
    if err != nil {
        panic(err)
    }
    fmt.Printf("DOM has %d nodes\n", len(snapshot.Documents))
    fmt.Printf("Snapshot strings: %v\n", snapshot.Strings[:10])

    // Browser version info
    version, err := browser.Version()
    if err != nil {
        panic(err)
    }
    fmt.Printf("Browser: %s %s\n", version.Product, version.Revision)
    fmt.Printf("User Agent: %s\n", version.UserAgent)
}
```

Rod provides a comprehensive Go library for browser automation that balances ease of use with powerful low-level control. The main use cases include web scraping at scale, end-to-end testing of web applications, automated form filling and data entry, monitoring and screenshot capture of web pages, and testing browser extensions. The library's automatic retry mechanisms, intelligent waiting strategies, and resource pooling make it particularly well-suited for production environments requiring reliability and performance. Rod's direct CDP integration allows developers to access any browser feature while maintaining clean, idiomatic Go code.

Integration with existing Go applications is straightforward through standard context patterns for cancellation and timeouts, making Rod compatible with common Go frameworks and testing libraries. The Must-prefixed convenience methods reduce boilerplate for scripts and rapid prototyping, while the standard error-returning variants provide fine-grained control for production code. Rod's support for both headless and headed modes with remote monitoring capabilities makes debugging straightforward, and the comprehensive event system enables sophisticated browser behavior analysis and custom automation workflows.


