package httpapi

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

const proxyRuntimePatch = `(function(){if(window.__nineEDProxyPatched)return;window.__nineEDProxyPatched=true;var proxyBase=%q;var remotePath=%q;var tabId=%q;function normalize(input){try{var value=String(input);if(/^(data|blob|javascript|mailto|tel):/i.test(value))return value;if(value.startsWith("//"))return value;if(value.startsWith(window.location.origin+"/")&&!value.startsWith(window.location.origin+proxyBase)){var parsed=new URL(value);return proxyBase+parsed.pathname.replace(/^\/+/,"")+(parsed.search||"")+(parsed.hash||"")}if(value.startsWith("/"))return proxyBase+value.slice(1);return value}catch{return input}}function notifyParent(type,payload){try{if(window.parent&&window.parent!==window){window.parent.postMessage(Object.assign({__nineBrowser:true,type:type,tabId:tabId},payload||{}),window.location.origin)}}catch{}}var originalFetch=window.fetch;if(typeof originalFetch==="function"){window.fetch=function(input,init){if(typeof input==="string"||input instanceof URL){input=normalize(input)}else if(input instanceof Request){input=new Request(normalize(input.url),input)}return originalFetch.call(this,input,init)}}var originalOpen=XMLHttpRequest.prototype.open;XMLHttpRequest.prototype.open=function(method,url){if(arguments.length>1){arguments[1]=normalize(url)}return originalOpen.apply(this,arguments)};if(window.EventSource){var OriginalEventSource=window.EventSource;window.EventSource=function(url,config){return new OriginalEventSource(normalize(url),config)};window.EventSource.prototype=OriginalEventSource.prototype}if(navigator.serviceWorker&&typeof navigator.serviceWorker.register==="function"){navigator.serviceWorker.register=function(){return Promise.resolve({active:null,installing:null,waiting:null,scope:proxyBase,update:function(){return Promise.resolve()},unregister:function(){return Promise.resolve(true)},addEventListener:function(){},removeEventListener:function(){}})}}function rewriteAttr(el,attr){var v=el.getAttribute(attr);if(v&&!v.startsWith(proxyBase)&&v.startsWith("/")){el.setAttribute(attr,normalize(v))}}var nativeCreateElement=document.createElement.bind(document);document.createElement=function(tag,opts){var el=nativeCreateElement(tag,opts);var tl=String(tag).toLowerCase();if(tl==="script"){rewriteAttr(el,"src")}else if(tl==="link"){rewriteAttr(el,"href")}return el};try{var scriptSrcDesc=Object.getOwnPropertyDescriptor(HTMLScriptElement.prototype,"src");if(scriptSrcDesc&&scriptSrcDesc.set){var origScriptSrcSet=scriptSrcDesc.set;Object.defineProperty(HTMLScriptElement.prototype,"src",{set:function(v){if(typeof v==="string"&&v.startsWith("/")){arguments[0]=normalize(v)}return origScriptSrcSet.apply(this,arguments)},get:scriptSrcDesc.get,configurable:true})}}catch{}try{var linkHrefDesc=Object.getOwnPropertyDescriptor(HTMLLinkElement.prototype,"href");if(linkHrefDesc&&linkHrefDesc.set){var origLinkHrefSet=linkHrefDesc.set;Object.defineProperty(HTMLLinkElement.prototype,"href",{set:function(v){if(typeof v==="string"&&v.startsWith("/")){arguments[0]=normalize(v)}return origLinkHrefSet.apply(this,arguments)},get:linkHrefDesc.get,configurable:true})}}catch{}new MutationObserver(function(mutations){for(var m=0;m<mutations.length;m++){for(var n=0;n<mutations[m].addedNodes.length;n++){var node=mutations[m].addedNodes[n];if(node.nodeType===1){if(node.tagName==="SCRIPT"){rewriteAttr(node,"src")}else if(node.tagName==="LINK"){rewriteAttr(node,"href")}else if(node.tagName==="IFRAME"||node.tagName==="IMG"||node.tagName==="VIDEO"||node.tagName==="SOURCE"){rewriteAttr(node,"src")}}}}}).observe(document.documentElement,{childList:true,subtree:true});var nativeWindowOpen=window.open;window.open=function(url,target,features){var normalized=normalize(url||"about:blank");var targetName=String(target||"_blank").toLowerCase();if(targetName===""||targetName==="_blank"||targetName==="_new"||features){notifyParent("open-tab",{url:normalized,target:target||"_blank",features:features||""});return {closed:false,close:function(){notifyParent("close-tab",{})},focus:function(){notifyParent("focus-tab",{})},postMessage:function(message,origin){notifyParent("post-message",{message:message,origin:origin||"*"})},location:{href:normalized}}}return nativeWindowOpen.call(window,normalized,target,features)};var nativeClose=window.close;window.close=function(){notifyParent("close-tab",{});try{return nativeClose.call(window)}catch{return undefined}};document.addEventListener("click",function(event){var node=event.target;while(node&&node.nodeType===1){if(node.tagName==="A"&&node.href){var targetName=String(node.getAttribute("target")||"").toLowerCase();if(targetName==="_blank"||node.hasAttribute("download")){event.preventDefault();notifyParent("open-tab",{url:normalize(node.href),target:targetName||"_blank"});return}break}node=node.parentElement}},true);document.addEventListener("submit",function(event){var form=event.target;if(form&&form.tagName==="FORM"){var targetName=String(form.getAttribute("target")||"").toLowerCase();if(targetName==="_blank"){event.preventDefault();var action=form.getAttribute("action")||remotePath||"/";notifyParent("open-tab",{url:normalize(action),target:"_blank",method:String(form.getAttribute("method")||"get").toUpperCase()})}}},true)})();`

const robustProxyRuntimePatch = `(function(){if(window.__nineEDProxyPatched)return;window.__nineEDProxyPatched=true;var proxyBase=%q;var remotePath=%q;var tabId=%q;var externalBase=proxyBase+"_proxy/";function proxiedExternal(u){return externalBase+u.protocol.replace(":","")+"/"+encodeURIComponent(u.host)+u.pathname+(u.search||"")+(u.hash||"")}function normalize(input){try{var value=String(input);if(!value||/^(data|blob|javascript|mailto|tel):/i.test(value))return value;if(value.startsWith(proxyBase)||value.startsWith(externalBase))return value;var u;if(value.startsWith("//")){u=new URL(window.location.protocol+value)}else{u=new URL(value,window.location.href)}if(u.origin===window.location.origin){if(u.pathname.startsWith(proxyBase))return u.pathname+(u.search||"")+(u.hash||"");return proxyBase+u.pathname.replace(/^\/+/,"")+(u.search||"")+(u.hash||"")}if(u.protocol==="http:"||u.protocol==="https:")return proxiedExternal(u);if(value.startsWith("/"))return proxyBase+value.slice(1);return value}catch{return input}}function notifyParent(type,payload){try{if(window.parent&&window.parent!==window){window.parent.postMessage(Object.assign({__nineBrowser:true,type:type,tabId:tabId},payload||{}),window.location.origin)}}catch{}}var originalFetch=window.fetch;if(typeof originalFetch==="function"){window.fetch=function(input,init){if(typeof input==="string"||input instanceof URL){input=normalize(input)}else if(input instanceof Request){input=new Request(normalize(input.url),input)}return originalFetch.call(this,input,init)}}var originalOpen=XMLHttpRequest.prototype.open;XMLHttpRequest.prototype.open=function(method,url){if(arguments.length>1){arguments[1]=normalize(url)}return originalOpen.apply(this,arguments)};if(window.EventSource){var OriginalEventSource=window.EventSource;window.EventSource=function(url,config){return new OriginalEventSource(normalize(url),config)};window.EventSource.prototype=OriginalEventSource.prototype}if(navigator.serviceWorker&&typeof navigator.serviceWorker.register==="function"){navigator.serviceWorker.register=function(){return Promise.resolve({active:null,installing:null,waiting:null,scope:proxyBase,update:function(){return Promise.resolve()},unregister:function(){return Promise.resolve(true)},addEventListener:function(){},removeEventListener:function(){}})}}function rewriteAttr(el,attr){var v=el.getAttribute(attr);if(v&&!v.startsWith(proxyBase)&&!v.startsWith(externalBase)){el.setAttribute(attr,normalize(v))}}var nativeCreateElement=document.createElement.bind(document);document.createElement=function(tag,opts){var el=nativeCreateElement(tag,opts);var tl=String(tag).toLowerCase();if(tl==="script"){rewriteAttr(el,"src")}else if(tl==="link"){rewriteAttr(el,"href")}return el};try{var scriptSrcDesc=Object.getOwnPropertyDescriptor(HTMLScriptElement.prototype,"src");if(scriptSrcDesc&&scriptSrcDesc.set){var origScriptSrcSet=scriptSrcDesc.set;Object.defineProperty(HTMLScriptElement.prototype,"src",{set:function(v){if(typeof v==="string"){arguments[0]=normalize(v)}return origScriptSrcSet.apply(this,arguments)},get:scriptSrcDesc.get,configurable:true})}}catch{}try{var linkHrefDesc=Object.getOwnPropertyDescriptor(HTMLLinkElement.prototype,"href");if(linkHrefDesc&&linkHrefDesc.set){var origLinkHrefSet=linkHrefDesc.set;Object.defineProperty(HTMLLinkElement.prototype,"href",{set:function(v){if(typeof v==="string"){arguments[0]=normalize(v)}return origLinkHrefSet.apply(this,arguments)},get:linkHrefDesc.get,configurable:true})}}catch{}new MutationObserver(function(mutations){for(var m=0;m<mutations.length;m++){for(var n=0;n<mutations[m].addedNodes.length;n++){var node=mutations[m].addedNodes[n];if(node.nodeType===1){if(node.tagName==="SCRIPT"){rewriteAttr(node,"src")}else if(node.tagName==="LINK"){rewriteAttr(node,"href")}else if(node.tagName==="IFRAME"||node.tagName==="IMG"||node.tagName==="VIDEO"||node.tagName==="SOURCE"){rewriteAttr(node,"src")}}}}}).observe(document.documentElement,{childList:true,subtree:true});var nativeWindowOpen=window.open;window.open=function(url,target,features){var normalized=normalize(url||"about:blank");var targetName=String(target||"_blank").toLowerCase();if(targetName===""||targetName==="_blank"||targetName==="_new"||features){notifyParent("open-tab",{url:normalized,target:target||"_blank",features:features||""});return {closed:false,close:function(){notifyParent("close-tab",{})},focus:function(){notifyParent("focus-tab",{})},postMessage:function(message,origin){notifyParent("post-message",{message:message,origin:origin||"*"})},location:{href:normalized}}}return nativeWindowOpen.call(window,normalized,target,features)};var nativeClose=window.close;window.close=function(){notifyParent("close-tab",{});try{return nativeClose.call(window)}catch{return undefined}};document.addEventListener("click",function(event){var node=event.target;while(node&&node.nodeType===1){if(node.tagName==="A"&&node.href){var targetName=String(node.getAttribute("target")||"").toLowerCase();if(targetName==="_blank"||node.hasAttribute("download")){event.preventDefault();notifyParent("open-tab",{url:normalize(node.href),target:targetName||"_blank"});return}break}node=node.parentElement}},true);document.addEventListener("submit",function(event){var form=event.target;if(form&&form.tagName==="FORM"){var targetName=String(form.getAttribute("target")||"").toLowerCase();if(targetName==="_blank"){event.preventDefault();var action=form.getAttribute("action")||remotePath||"/";notifyParent("open-tab",{url:normalize(action),target:"_blank",method:String(form.getAttribute("method")||"get").toUpperCase()})}}},true)})();`

func rewriteProxyResponseBody(resp *http.Response, prefix string, remotePath string, tabID string) error {
	if resp.Body == nil {
		return nil
	}

	if location := resp.Header.Get("Location"); location != "" {
		resp.Header.Set("Location", rewriteProxyLocation(location, prefix))
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(contentType, "text/html") && !strings.Contains(contentType, "text/css") && !isJavaScriptContentType(contentType) {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		resp.Body.Close()
		return err
	}
	_ = resp.Body.Close()

	rewritten := body
	if strings.Contains(contentType, "text/html") {
		rewritten, err = rewriteProxyHTML(body, prefix, remotePath, tabID)
		if err != nil {
			return err
		}
	} else if strings.Contains(contentType, "text/css") {
		rewritten = rewriteProxyCSS(body, prefix)
	} else if isJavaScriptContentType(contentType) {
		rewritten = rewriteProxyJavaScript(body, prefix)
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

	if rewritten := rewriteProxyExternalURL(location, prefix); rewritten != "" {
		return rewritten
	}
	return location
}

func rewriteProxyHTML(input []byte, prefix string, remotePath string, tabID string) ([]byte, error) {
	doc, err := html.Parse(bytes.NewReader(input))
	if err != nil {
		return nil, err
	}

	head := findElement(doc, "head")
	body := findElement(doc, "body")
	if head != nil {
		ensureProxyBase(head, proxyDocumentBase(prefix, remotePath))
		injectProxyRuntime(head, prefix, remotePath, tabID)
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
			// Rewrite inline <style> content — CSS url() with root-relative paths
			if node.Data == "style" {
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					if child.Type == html.TextNode {
						child.Data = string(rewriteProxyCSS([]byte(child.Data), prefix))
					}
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

func rewriteProxyCSS(input []byte, prefix string) []byte {
	css := string(input)
	var output strings.Builder
	output.Grow(len(css) + len(prefix)*2)

	for i := 0; i < len(css); {
		idx := strings.Index(strings.ToLower(css[i:]), "url(")
		if idx < 0 {
			output.WriteString(css[i:])
			break
		}
		idx += i
		output.WriteString(css[i:idx])

		end := strings.IndexByte(css[idx:], ')')
		if end < 0 {
			output.WriteString(css[idx:])
			break
		}
		end += idx

		segment := css[idx : end+1]
		rewritten := segment
		open := strings.IndexByte(segment, '(')
		close := strings.LastIndexByte(segment, ')')
		if open >= 0 && close > open {
			inner := strings.TrimSpace(segment[open+1 : close])
			quote := ""
			value := inner
			if len(inner) >= 2 {
				if (inner[0] == '\'' && inner[len(inner)-1] == '\'') || (inner[0] == '"' && inner[len(inner)-1] == '"') {
					quote = inner[:1]
					value = inner[1 : len(inner)-1]
				}
			}
			lower := strings.ToLower(value)
			if rewrittenURL := rewriteProxyExternalURL(value, prefix); rewrittenURL != "" {
				rewritten = "url(" + quote + rewrittenURL + quote + ")"
			} else if strings.HasPrefix(value, "/") && !strings.HasPrefix(lower, "//") {
				rewritten = "url(" + quote + prefix + strings.TrimPrefix(value, "/") + quote + ")"
			}
		}
		output.WriteString(rewritten)
		i = end + 1
	}

	return []byte(output.String())
}

// jsURLPattern matches quoted root-relative URL strings that look like actual asset paths.
// Matches either: (a) 2+ path segments like /assets/chunk.js, or (b) 1 segment with a file extension like /sw.js.
// This avoids rewriting bare "/" or non-path strings.
var jsURLPattern = regexp.MustCompile(
	"([\"'\\x60])((?:/[a-zA-Z0-9._~-]+){2,}|/[a-zA-Z0-9._~-]+\\.[a-zA-Z0-9._~-]+)(?:\\?[^\"'\\x60]*)?(?:#[^\"'\\x60]*)?",
)

func rewriteProxyJavaScript(input []byte, prefix string) []byte {
	script := string(input)

	result := jsURLPattern.ReplaceAllStringFunc(script, func(match string) string {
		// Determine quote char and inner content
		quote := string(match[0])
		inner := match[1:]

		// Skip protocol-relative
		if strings.HasPrefix(inner, "//") {
			return match
		}
		// Skip already rewritten
		if strings.HasPrefix(inner, prefix) {
			return match
		}
		// Skip data/blob/javascript URIs
		lower := strings.ToLower(inner)
		if strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "blob:") ||
			strings.HasPrefix(lower, "javascript:") || strings.HasPrefix(lower, "mailto:") {
			return match
		}

		// Rewrite: "/path" → "{prefix}path"
		return quote + prefix + strings.TrimPrefix(inner, "/")
	})

	return []byte(result)
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
		return value
	}
	if strings.HasPrefix(value, "/") {
		return prefix + strings.TrimPrefix(value, "/")
	}
	if rewritten := rewriteProxyExternalURL(value, prefix); rewritten != "" {
		return rewritten
	}
	return value
}

func rewriteProxyExternalURL(value string, prefix string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	pathPart := parsed.EscapedPath()
	if pathPart == "" {
		pathPart = "/"
	}
	rewritten := browserTabProxyBase(prefix) + "_proxy/" + scheme + "/" + url.PathEscape(parsed.Host) + pathPart
	if parsed.RawQuery != "" {
		rewritten += "?" + parsed.RawQuery
	}
	if parsed.Fragment != "" {
		rewritten += "#" + parsed.Fragment
	}
	return rewritten
}

func browserTabProxyBase(prefix string) string {
	if before, _, found := strings.Cut(prefix, "_proxy/"); found {
		return before
	}
	return prefix
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

func proxyDocumentBase(prefix string, remotePath string) string {
	remoteURL, err := url.Parse(remotePath)
	if err != nil {
		return prefix
	}
	remoteDir := path.Dir(remoteURL.Path)
	if remoteDir == "." || remoteDir == "/" {
		return prefix
	}
	return prefix + strings.Trim(remoteDir, "/") + "/"
}

func injectProxyRuntime(head *html.Node, prefix string, remotePath string, tabID string) {
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
		Data: fmt.Sprintf(robustProxyRuntimePatch, prefix, remotePath, tabID),
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
	return path.Join("/browser", tabID) + "/"
}

func browserExternalProxyPrefix(tabID string, scheme string, host string) string {
	return browserProxyPrefix(tabID) + "_proxy/" + strings.ToLower(scheme) + "/" + url.PathEscape(host) + "/"
}

func isJavaScriptContentType(contentType string) bool {
	return strings.Contains(contentType, "javascript") || strings.Contains(contentType, "ecmascript") || strings.Contains(contentType, "application/x-javascript")
}
