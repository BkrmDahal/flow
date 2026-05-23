package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// WebSearchTool queries DuckDuckGo HTML search to return lightweight, zero-config web results.
type WebSearchTool struct{}

func (t *WebSearchTool) Name() string { return "web_search" }

func (t *WebSearchTool) Description() string {
	return "Search the web for up-to-date information, documentation, news, or errors using a search query. Returns a list of web page search results with titles, snippets, and URLs."
}

func (t *WebSearchTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "The search query (e.g. 'Golang net/http timeout best practices')",
			},
		},
		"required": []string{"query"},
	}
}

type webSearchInput struct {
	Query string `json:"query"`
}

func (t *WebSearchTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var in webSearchInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}

	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(in.Query))
	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return fmt.Sprintf("Error creating search request: %v", err), nil
	}

	// Use a standard browser User-Agent so DuckDuckGo serves the standard HTML page.
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("Error executing search query: %v", err), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("Search provider returned non-OK status: %s", resp.Status), nil
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("Error reading search results: %v", err), nil
	}

	htmlContent := string(bodyBytes)
	results := parseDuckDuckGoHTML(htmlContent)

	if len(results) == 0 {
		return "No search results found. Try refining your query.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search results for %q:\n\n", in.Query))
	for i, r := range results {
		if i >= 8 { // limit to top 8 results
			break
		}
		sb.WriteString(fmt.Sprintf("[%d] %s\n    URL: %s\n    Snippet: %s\n\n", i+1, r.Title, r.URL, r.Snippet))
	}

	return sb.String(), nil
}

type searchResult struct {
	Title   string
	URL     string
	Snippet string
}

func parseDuckDuckGoHTML(html string) []searchResult {
	var results []searchResult

	// In DDG HTML Search, result blocks are usually divs with class result__body
	blocks := strings.Split(html, "class=\"result__body\"")
	if len(blocks) <= 1 {
		// Fallback to simpler table splitting if the page structure changed
		blocks = strings.Split(html, "class=\"web-result\"")
	}

	for _, block := range blocks[1:] {
		// Extract URL and Title
		// Format: <a class="result__url" href="[URL]">...</a> or similar
		var r searchResult

		// Link & Title parsing
		linkIdx := strings.Index(block, "href=\"")
		if linkIdx == -1 {
			continue
		}
		linkPart := block[linkIdx+6:]
		linkEnd := strings.Index(linkPart, "\"")
		if linkEnd == -1 {
			continue
		}
		rawURL := linkPart[:linkEnd]

		// Resolve actual URL from DDG proxy redirects if present (e.g. //duckduckgo.com/l/?uddg=URL)
		if strings.Contains(rawURL, "uddg=") {
			u, err := url.Parse(rawURL)
			if err == nil {
				resolvedURL := u.Query().Get("uddg")
				if resolvedURL != "" {
					r.URL = resolvedURL
				} else {
					r.URL = rawURL
				}
			} else {
				r.URL = rawURL
			}
		} else {
			r.URL = rawURL
		}

		// DuckDuckGo internal links fallback
		if strings.HasPrefix(r.URL, "//") {
			r.URL = "https:" + r.URL
		}

		// Title parsing: look for result__title class
		titleIdx := strings.Index(block, "class=\"result__snippet\"")
		// The title is in the link inside class result__a
		aIdx := strings.Index(block, "class=\"result__a\"")
		if aIdx != -1 {
			aPart := block[aIdx:]
			tagClose := strings.Index(aPart, ">")
			if tagClose != -1 {
				aContent := aPart[tagClose+1:]
				aEnd := strings.Index(aContent, "</a>")
				if aEnd != -1 {
					r.Title = stripHTMLTags(aContent[:aEnd])
				}
			}
		}

		// Snippet parsing: class result__snippet
		if titleIdx != -1 {
			snippetPart := block[titleIdx:]
			tagClose := strings.Index(snippetPart, ">")
			if tagClose != -1 {
				snippetContent := snippetPart[tagClose+1:]
				snippetEnd := strings.Index(snippetContent, "</a>")
				if snippetEnd == -1 {
					snippetEnd = strings.Index(snippetContent, "</div>")
				}
				if snippetEnd != -1 {
					r.Snippet = stripHTMLTags(snippetContent[:snippetEnd])
				}
			}
		}

		r.Title = strings.TrimSpace(r.Title)
		r.URL = strings.TrimSpace(r.URL)
		r.Snippet = strings.TrimSpace(r.Snippet)

		if r.URL != "" && r.Title != "" {
			results = append(results, r)
		}
	}

	return results
}

// FetchURLTool downloads the content of a URL and converts the text/HTML to clean markdown representation.
type FetchURLTool struct{}

func (t *FetchURLTool) Name() string { return "fetch_url" }

func (t *FetchURLTool) Description() string {
	return "Download the readable contents of a web page URL. Stalks technical documentation, tutorials, issues, or scripts directly. Output is parsed to clean text and truncated to 15KB."
}

func (t *FetchURLTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "The URL to fetch (e.g. 'https://go.dev/doc/tutorial/web-service-gin')",
			},
		},
		"required": []string{"url"},
	}
}

type fetchURLInput struct {
	URL string `json:"url"`
}

func (t *FetchURLTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var in fetchURLInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}

	targetURL := in.URL
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		targetURL = "https://" + targetURL
	}

	// SSRF protection: block requests to internal/private network addresses.
	if err := validateURLTarget(targetURL); err != nil {
		return fmt.Sprintf("Error: %v", err), nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return fmt.Sprintf("Error creating request: %v", err), nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("Error fetching webpage: %v", err), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("Server returned error status: %s", resp.Status), nil
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("Error reading page contents: %v", err), nil
	}

	rawHTML := string(bodyBytes)
	cleanText := convertHTMLToText(rawHTML)

	if len(cleanText) > 15*1024 {
		cleanText = cleanText[:15*1024] + "\n\n... [output truncated at 15KB to save context tokens]"
	}

	return fmt.Sprintf("URL: %s\n\nContent:\n%s", targetURL, cleanText), nil
}

// validateURLTarget blocks requests to internal/private IP ranges to
// prevent SSRF attacks via prompt injection.
func validateURLTarget(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Only allow http and https schemes.
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("only http:// and https:// URLs are allowed")
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL has no hostname")
	}

	// Block well-known internal hostnames.
	lowerHost := strings.ToLower(host)
	blockedHosts := []string{"localhost", "metadata.google.internal"}
	for _, blocked := range blockedHosts {
		if lowerHost == blocked {
			return fmt.Errorf("access to %s is blocked for security reasons", host)
		}
	}

	// Resolve the hostname to IP to check against private ranges.
	// This also prevents DNS rebinding attacks.
	ips, err := net.LookupIP(host)
	if err != nil {
		// If we can't resolve it, let the HTTP client handle the error naturally.
		return nil
	}

	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("access to internal/private network address %s (%s) is blocked", host, ip.String())
		}
	}

	return nil
}

// isPrivateIP returns true if the given IP is in a private, loopback,
// link-local, or cloud metadata range.
func isPrivateIP(ip net.IP) bool {
	privateRanges := []string{
		"127.0.0.0/8",    // Loopback
		"10.0.0.0/8",     // Private Class A
		"172.16.0.0/12",  // Private Class B
		"192.168.0.0/16", // Private Class C
		"169.254.0.0/16", // Link-local / cloud metadata
		"::1/128",        // IPv6 loopback
		"fc00::/7",       // IPv6 unique local
		"fe80::/10",      // IPv6 link-local
	}

	for _, cidr := range privateRanges {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func convertHTMLToText(html string) string {
	// Remove script blocks
	reScript := regexp.MustCompile(`(?s)<script.*?>.*?</script>`)
	html = reScript.ReplaceAllString(html, "")

	// Remove style blocks
	reStyle := regexp.MustCompile(`(?s)<style.*?>.*?</style>`)
	html = reStyle.ReplaceAllString(html, "")

	// Strip standard HTML tags, replacing block structures with line breaks
	html = strings.ReplaceAll(html, "<p>", "\n\n")
	html = strings.ReplaceAll(html, "</p>", "")
	html = strings.ReplaceAll(html, "<br>", "\n")
	html = strings.ReplaceAll(html, "<br/>", "\n")
	html = strings.ReplaceAll(html, "<tr>", "\n")
	html = strings.ReplaceAll(html, "</td>", " | ")
	html = strings.ReplaceAll(html, "</h1>", "\n\n")
	html = strings.ReplaceAll(html, "</h2>", "\n\n")
	html = strings.ReplaceAll(html, "</h3>", "\n\n")

	clean := stripHTMLTags(html)

	// Clean up duplicate spaces and newlines
	lines := strings.Split(clean, "\n")
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}

	return strings.Join(out, "\n")
}

func stripHTMLTags(src string) string {
	// Strip HTML tags using regex
	re := regexp.MustCompile(`<[^>]*>`)
	src = re.ReplaceAllString(src, " ")

	// Replace common HTML entities
	src = strings.ReplaceAll(src, "&nbsp;", " ")
	src = strings.ReplaceAll(src, "&lt;", "<")
	src = strings.ReplaceAll(src, "&gt;", ">")
	src = strings.ReplaceAll(src, "&amp;", "&")
	src = strings.ReplaceAll(src, "&quot;", "\"")
	src = strings.ReplaceAll(src, "&#39;", "'")
	src = strings.ReplaceAll(src, "&apos;", "'")

	// Collapse multiple spaces
	reSpace := regexp.MustCompile(`[ \t]+`)
	src = reSpace.ReplaceAllString(src, " ")

	return src
}
