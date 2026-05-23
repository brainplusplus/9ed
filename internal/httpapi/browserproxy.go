package httpapi

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"golang.org/x/net/html"
)

const proxyRuntimePatch = `(function(){if(window.__nineEDProxyPatched)return;window.__nineEDProxyPatched=true;var proxyBase=%q;function normalize(input){try{var value=String(input);if(/^(data|blob|javascript|mailto|tel):/i.test(value))return value;if(value.startsWith("//"))return window.location.protocol+value;if(value.startsWith("/"))return proxyBase+value.slice(1);return value}catch{return input}}var originalFetch=window.fetch;if(typeof originalFetch==="function"){window.fetch=function(input,init){if(typeof input==="string"){input=normalize(input)}else if(input instanceof Request){input=new Request(normalize(input.url),input)}return originalFetch.call(this,input,init)}}var originalOpen=XMLHttpRequest.prototype.open;XMLHttpRequest.prototype.open=function(method,url){if(arguments.length>1){arguments[1]=normalize(url)}return originalOpen.apply(this,arguments)};if(window.EventSource){var OriginalEventSource=window.EventSource;window.EventSource=function(url,config){return new OriginalEventSource(normalize(url),config)};window.EventSource.prototype=OriginalEventSource.prototype}})();`

func rewriteProxyResponseBody(resp *http.Response, prefix string) error {
	if resp.Body == nil {
		return nil
	}

	if location := resp.Header.Get("Location"); location != "" {
		resp.Header.Set("Location", rewriteProxyLocation(location, prefix))
	}

	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		resp.Body.Close()
		return err
	}
	_ = resp.Body.Close()

	rewritten, err := rewriteProxyHTML(body, prefix)
	if err != nil {
		return err
	}

	resp.Body = io.NopCloser(bytes.NewReader(rewritten))
	resp.ContentLength = int64(len(rewritten))
	resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(rewritten)))
	return nil
}

func rewriteProxyLocation(location string, prefix string) string {
	if location == "" {
		return location
	}
	if strings.HasPrefix(location, "data:") || strings.HasPrefix(location, "javascript:") {
		return location
	}
	if strings.HasPrefix(location, "/") {
		return prefix + strings.TrimPrefix(location, "/")
	}

	parsed, err := url.Parse(location)
	if err != nil {
		return location
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return location
	}
	rewritten := prefix + strings.TrimPrefix(parsed.Path, "/")
	if parsed.RawQuery != "" {
		rewritten += "?" + parsed.RawQuery
	}
	if parsed.Fragment != "" {
		rewritten += "#" + parsed.Fragment
	}
	return rewritten
}

func rewriteProxyHTML(input []byte, prefix string) ([]byte, error) {
	doc, err := html.Parse(bytes.NewReader(input))
	if err != nil {
		return nil, err
	}

	head := findElement(doc, "head")
	body := findElement(doc, "body")
	if head != nil {
		ensureProxyBase(head, prefix)
		injectProxyRuntime(head, prefix)
	}

	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			for i := range node.Attr {
				switch node.Attr[i].Key {
				case "href", "src", "action", "poster":
					node.Attr[i].Val = rewriteProxyAttribute(node.Attr[i].Val, prefix)
				case "srcset":
					node.Attr[i].Val = rewriteProxySrcset(node.Attr[i].Val, prefix)
				}
			}
			if node.Data == "base" && body != nil && head != nil && node.Parent == head {
				moveNodeToFront(head, node)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	var output bytes.Buffer
	if err := html.Render(&output, doc); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func rewriteProxyAttribute(value string, prefix string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "blob:") || strings.HasPrefix(lower, "javascript:") || strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "tel:") || strings.HasPrefix(value, "#") {
		return value
	}
	if strings.HasPrefix(value, prefix) {
		return value
	}
	if strings.HasPrefix(value, "//") {
		return prefix + strings.TrimPrefix(value, "//")
	}
	if strings.HasPrefix(value, "/") {
		return prefix + strings.TrimPrefix(value, "/")
	}
	return value
}

func rewriteProxySrcset(value string, prefix string) string {
	parts := strings.Split(value, ",")
	for i, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) == 0 {
			continue
		}
		fields[0] = rewriteProxyAttribute(fields[0], prefix)
		parts[i] = strings.Join(fields, " ")
	}
	return strings.Join(parts, ", ")
}

func ensureProxyBase(head *html.Node, prefix string) {
	for child := head.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == "base" {
			setAttr(child, "href", prefix)
			moveNodeToFront(head, child)
			return
		}
	}

	base := &html.Node{
		Type: html.ElementNode,
		Data: "base",
		Attr: []html.Attribute{{Key: "href", Val: prefix}},
	}
	head.InsertBefore(base, head.FirstChild)
}

func injectProxyRuntime(head *html.Node, prefix string) {
	for child := head.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == "script" && attrValue(child, "data-nine-proxy-runtime") == "true" {
			return
		}
	}

	script := &html.Node{
		Type: html.ElementNode,
		Data: "script",
		Attr: []html.Attribute{{Key: "data-nine-proxy-runtime", Val: "true"}},
	}
	script.AppendChild(&html.Node{
		Type: html.TextNode,
		Data: fmt.Sprintf(proxyRuntimePatch, prefix),
	})
	if head.FirstChild != nil {
		head.InsertBefore(script, head.FirstChild.NextSibling)
		return
	}
	head.AppendChild(script)
}

func findElement(root *html.Node, name string) *html.Node {
	var walk func(*html.Node) *html.Node
	walk = func(node *html.Node) *html.Node {
		if node.Type == html.ElementNode && node.Data == name {
			return node
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if found := walk(child); found != nil {
				return found
			}
		}
		return nil
	}
	return walk(root)
}

func moveNodeToFront(parent *html.Node, node *html.Node) {
	if parent == nil || node == nil || parent.FirstChild == node {
		return
	}
	parent.RemoveChild(node)
	parent.InsertBefore(node, parent.FirstChild)
}

func setAttr(node *html.Node, key string, value string) {
	for i := range node.Attr {
		if node.Attr[i].Key == key {
			node.Attr[i].Val = value
			return
		}
	}
	node.Attr = append(node.Attr, html.Attribute{Key: key, Val: value})
}

func attrValue(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func browserProxyPrefix(tabID string) string {
	return path.Join("/api/browser/proxy", tabID) + "/"
}
