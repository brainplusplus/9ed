package browser

import (
	"encoding/json"
	"fmt"
	"strings"

	playwright "github.com/playwright-community/playwright-go"
)

type BoxModelEdges struct {
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
	Left   float64 `json:"left"`
}

type ContentRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type BoxModel struct {
	Margin      BoxModelEdges `json:"margin"`
	Border      BoxModelEdges `json:"border"`
	Padding     BoxModelEdges `json:"padding"`
	ContentRect ContentRect   `json:"contentRect"`
}

type ParentChainItem struct {
	TagName string   `json:"tagName"`
	ID      string   `json:"id,omitempty"`
	Classes []string `json:"classes"`
}

type AccessibilityInfo struct {
	Role          string `json:"role,omitempty"`
	Label         string `json:"label,omitempty"`
	TabIndex      *int   `json:"tabIndex,omitempty"`
	Focusable     bool   `json:"focusable"`
	ContrastRatio string `json:"contrastRatio,omitempty"`
}

type EventListenerInfo struct {
	Type        string `json:"type"`
	HandlerBody string `json:"handlerBody"`
}

type ComputedStyle struct {
	Display         string `json:"display"`
	Position        string `json:"position"`
	Width           string `json:"width"`
	Height          string `json:"height"`
	Color           string `json:"color"`
	BackgroundColor string `json:"backgroundColor"`
	FontSize        string `json:"fontSize"`
	FontFamily      string `json:"fontFamily"`
	FontWeight      string `json:"fontWeight"`
	LineHeight      string `json:"lineHeight"`
	TextAlign       string `json:"textAlign"`
	Margin          string `json:"margin"`
	Padding         string `json:"padding"`
	Border          string `json:"border"`
	BorderRadius    string `json:"borderRadius"`
	Overflow        string `json:"overflow"`
	Opacity         string `json:"opacity"`
	Visibility      string `json:"visibility"`
	ZIndex          string `json:"zIndex"`
	Flex            string `json:"flex,omitempty"`
	Grid            string `json:"grid,omitempty"`
	Gap             string `json:"gap,omitempty"`
	Top             string `json:"top,omitempty"`
	Left            string `json:"left,omitempty"`
	Right           string `json:"right,omitempty"`
	Bottom          string `json:"bottom,omitempty"`
}

type BoundingRect struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
}

type ElementSelection struct {
	TabID             string              `json:"tabId,omitempty"`
	URL               string              `json:"url"`
	Title             string              `json:"title"`
	TagName           string              `json:"tagName"`
	Role              string              `json:"role,omitempty"`
	Text              string              `json:"text,omitempty"`
	Selector          string              `json:"selector"`
	UniqueSelector    string              `json:"uniqueSelector,omitempty"`
	OuterHTML         string              `json:"outerHTML"`
	Attributes        map[string]string   `json:"attributes,omitempty"`
	ComputedStyle     *ComputedStyle      `json:"computedStyle,omitempty"`
	BoundingRect      *BoundingRect       `json:"boundingRect,omitempty"`
	BoxModel          *BoxModel           `json:"boxModel,omitempty"`
	ParentChain       []ParentChainItem   `json:"parentChain,omitempty"`
	AccessibilityInfo *AccessibilityInfo  `json:"accessibilityInfo,omitempty"`
	EventListeners    []EventListenerInfo `json:"eventListeners,omitempty"`
}

func inspectSelectionAtPoint(page playwright.Page, tab Tab, x float64, y float64) (ElementSelection, error) {
	result, err := page.Evaluate(`(input) => {
		const eventTypes = [
			'click', 'dblclick', 'mousedown', 'mouseup', 'mouseover', 'mousemove', 'mouseout',
			'keydown', 'keyup', 'keypress', 'focus', 'blur', 'change', 'input', 'submit',
			'scroll', 'resize', 'load', 'error', 'touchstart', 'touchend', 'touchmove',
		];
		const meaningfulAttrs = new Set([
			'id', 'role', 'type', 'name', 'value', 'href', 'src', 'alt', 'title',
			'placeholder', 'disabled', 'readonly', 'required', 'checked', 'selected',
			'aria-label', 'aria-labelledby', 'aria-describedby', 'aria-expanded',
			'aria-selected', 'aria-checked', 'aria-hidden', 'aria-disabled',
			'data-testid', 'data-id', 'for', 'action', 'method', 'target', 'rel',
			'download', 'pattern', 'min', 'max', 'step', 'maxlength'
		]);
		const resolveTargetElement = (raw) => {
			if (!raw || raw.nodeType !== 1) return null;
			let el = raw;
			while (el) {
				const cs = window.getComputedStyle(el);
				if (cs.display === 'none' || cs.visibility === 'hidden' || parseFloat(cs.opacity || '1') === 0) {
					el = el.parentElement;
					continue;
				}
				if (cs.pointerEvents === 'none' && el !== document.body && el !== document.documentElement) {
					el = el.parentElement;
					continue;
				}
				const rect = el.getBoundingClientRect();
				if (rect.width > 0 && rect.height > 0) return el;
				el = el.parentElement;
			}
			return null;
		};
		const isInteractiveElement = (el) => {
			if (!el || el.nodeType !== 1) return false;
			const tag = el.tagName.toLowerCase();
			if (['button', 'a', 'input', 'select', 'textarea', 'summary', 'option', 'label'].includes(tag)) return true;
			const role = (el.getAttribute('role') || '').toLowerCase();
			if (['button', 'link', 'menuitem', 'tab', 'checkbox', 'radio', 'switch', 'option'].includes(role)) return true;
			if (el.hasAttribute('contenteditable') && el.getAttribute('contenteditable') !== 'false') return true;
			if (typeof el.onclick === 'function' || !!el.getAttribute('onclick')) return true;
			const tabIndexAttr = el.getAttribute('tabindex');
			if (tabIndexAttr !== null) {
				const tabIndex = parseInt(tabIndexAttr, 10);
				if (!Number.isNaN(tabIndex) && tabIndex >= 0) return true;
			}
			const cs = window.getComputedStyle(el);
			if (cs.cursor === 'pointer') return true;
			return false;
		};
		const findInteractiveAncestor = (el) => {
			let current = el;
			while (current && current !== document.documentElement) {
				if (isInteractiveElement(current)) return current;
				current = current.parentElement;
			}
			return null;
		};
		const pickElementAtPoint = (x, y) => {
			const baseStack = typeof document.elementsFromPoint === 'function'
				? document.elementsFromPoint(x, y)
				: [document.elementFromPoint(x, y)];
			const points = [[0, 0], [1, 0], [-1, 0], [0, 1], [0, -1], [2, 0], [-2, 0], [0, 2], [0, -2]];
			const stack = [];
			for (const [dx, dy] of points) {
				const probeX = x + dx;
				const probeY = y + dy;
				const entries = (dx === 0 && dy === 0 && baseStack.length > 0)
					? baseStack
					: (typeof document.elementsFromPoint === 'function'
						? document.elementsFromPoint(probeX, probeY)
						: [document.elementFromPoint(probeX, probeY)]);
				for (const entry of entries) {
					if (entry && entry.nodeType === 1) stack.push(entry);
				}
			}
			let fallback = null;
			for (const raw of stack) {
				const resolved = resolveTargetElement(raw);
				if (!resolved) continue;
				if (!fallback) fallback = resolved;
				const interactive = findInteractiveAncestor(resolved);
				if (interactive) return interactive;
			}
			return fallback;
		};
		const element = pickElementAtPoint(input.x, input.y);
		if (!element) return null;

		const cssEscape = (value) => {
			try { return CSS.escape(value); } catch { return value; }
		};
		const buildSelectorPath = (el) => {
			const parts = [];
			let current = el;
			while (current && parts.length < 5) {
				let part = current.tagName.toLowerCase();
				if (current.id) {
					part += '#' + current.id;
					parts.unshift(part);
					break;
				}
				const cls = (current.getAttribute('class') || '').trim().split(/\s+/).filter(Boolean).slice(0, 2).join('.');
				if (cls) part += '.' + cls;
				parts.unshift(part);
				current = current.parentElement;
			}
			return parts.join(' > ');
		};
		const buildUniqueSelector = (el) => {
			const parts = [];
			let current = el;
			let depth = 0;
			while (current && current !== document.documentElement && depth < 8) {
				let part = current.tagName.toLowerCase();
				if (current.id) {
					part += '#' + cssEscape(current.id);
					parts.unshift(part);
					break;
				}
				const parent = current.parentElement;
				if (parent) {
					const siblings = Array.from(parent.children);
					const sameTag = siblings.filter((entry) => entry.tagName === current.tagName);
					if (sameTag.length > 1) {
						const idx = sameTag.indexOf(current) + 1;
						part += ':nth-child(' + idx + ')';
					}
				}
				const cls = (current.getAttribute('class') || '').trim().split(/\s+/).filter(Boolean).slice(0, 2);
				if (cls.length > 0) {
					part += cls.map((name) => '.' + cssEscape(name)).join('');
				}
				parts.unshift(part);
				current = parent;
				depth++;
			}
			if (current === document.documentElement) parts.unshift('html');
			return parts.join(' > ');
		};
		const buildParentChain = (el) => {
			const chain = [];
			let current = el;
			let depth = 0;
			while (current && current !== document.documentElement && depth < 6) {
				chain.unshift({
					tagName: current.tagName.toLowerCase(),
					id: current.id || undefined,
					classes: (current.getAttribute('class') || '').trim().split(/\s+/).filter(Boolean),
				});
				current = current.parentElement;
				depth++;
			}
			return chain;
		};
		const extractBoxModel = (el) => {
			const cs = window.getComputedStyle(el);
			const rect = el.getBoundingClientRect();
			const parse = (value) => parseFloat(value) || 0;
			const mt = parse(cs.marginTop), mr = parse(cs.marginRight), mb = parse(cs.marginBottom), ml = parse(cs.marginLeft);
			const bt = parse(cs.borderTopWidth), br = parse(cs.borderRightWidth), bb = parse(cs.borderBottomWidth), bl = parse(cs.borderLeftWidth);
			const pt = parse(cs.paddingTop), pr = parse(cs.paddingRight), pb = parse(cs.paddingBottom), pl = parse(cs.paddingLeft);
			const contentWidth = Math.max(0, rect.width - bl - br - pl - pr);
			const contentHeight = Math.max(0, rect.height - bt - bb - pt - pb);
			return {
				margin: { top: mt, right: mr, bottom: mb, left: ml },
				border: { top: bt, right: br, bottom: bb, left: bl },
				padding: { top: pt, right: pr, bottom: pb, left: pl },
				contentRect: {
					x: rect.x + bl + pl,
					y: rect.y + bt + pt,
					width: contentWidth,
					height: contentHeight,
				},
			};
		};
		const extractAccessibilityInfo = (el) => {
			const cs = window.getComputedStyle(el);
			const role = el.getAttribute('role') || undefined;
			const ariaLabel = el.getAttribute('aria-label') || el.getAttribute('aria-labelledby') || undefined;
			const tabIndexAttr = el.getAttribute('tabindex');
			const tabIndex = tabIndexAttr !== null ? parseInt(tabIndexAttr, 10) : undefined;
			const focusableTags = new Set(['a', 'button', 'input', 'select', 'textarea', 'details', 'summary']);
			const focusable = focusableTags.has(el.tagName.toLowerCase()) || (tabIndexAttr !== null && (tabIndex || -1) >= 0);
			let contrastRatio;
			if (cs.color && cs.backgroundColor && cs.backgroundColor !== 'rgba(0, 0, 0, 0)' && cs.backgroundColor !== 'transparent') {
				contrastRatio = cs.color + ' on ' + cs.backgroundColor;
			}
			return {
				role,
				label: ariaLabel,
				tabIndex,
				focusable,
				contrastRatio,
			};
		};
		const extractEventListeners = (el) => {
			const listeners = [];
			for (const type of eventTypes) {
				const attr = el.getAttribute('on' + type);
				if (attr) listeners.push({ type, handlerBody: attr.slice(0, 200) });
			}
			return listeners;
		};
		const attrs = {};
		for (const attr of Array.from(element.attributes)) {
			if (meaningfulAttrs.has(attr.name) || (attr.name.startsWith('data-') && attr.name !== 'data-nine-proxy-runtime')) {
				attrs[attr.name] = attr.value;
			}
		}
		const rect = element.getBoundingClientRect();
		const cs = window.getComputedStyle(element);
		const text = (element.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 600);
		const computedStyle = {
			display: cs.display,
			position: cs.position,
			width: cs.width,
			height: cs.height,
			color: cs.color,
			backgroundColor: cs.backgroundColor,
			fontSize: cs.fontSize,
			fontFamily: cs.fontFamily,
			fontWeight: cs.fontWeight,
			lineHeight: cs.lineHeight,
			textAlign: cs.textAlign,
			margin: cs.margin,
			padding: cs.padding,
			border: cs.border,
			borderRadius: cs.borderRadius,
			overflow: cs.overflow,
			opacity: cs.opacity,
			visibility: cs.visibility,
			zIndex: cs.zIndex,
		};
		if (cs.display.includes('flex')) computedStyle.flex = cs.flex;
		if (cs.display.includes('grid')) computedStyle.grid = cs.gridTemplateColumns;
		if (cs.gap !== 'normal') computedStyle.gap = cs.gap;
		if (cs.position !== 'static') {
			computedStyle.top = cs.top;
			computedStyle.left = cs.left;
			computedStyle.right = cs.right;
			computedStyle.bottom = cs.bottom;
		}
		return {
			tabId: input.tabId,
			url: input.url,
			title: input.title,
			tagName: element.tagName,
			role: element.getAttribute('role') || undefined,
			text: text || undefined,
			selector: buildSelectorPath(element),
			uniqueSelector: buildUniqueSelector(element),
			outerHTML: element.outerHTML.slice(0, 4000),
			attributes: Object.keys(attrs).length > 0 ? attrs : undefined,
			computedStyle,
			boundingRect: {
				width: rect.width,
				height: rect.height,
				x: rect.x,
				y: rect.y,
			},
			boxModel: extractBoxModel(element),
			parentChain: buildParentChain(element),
			accessibilityInfo: extractAccessibilityInfo(element),
			eventListeners: extractEventListeners(element),
		};
	}`, map[string]any{
		"x":     x,
		"y":     y,
		"tabId": tab.ID,
		"url":   tab.URL,
		"title": tab.Title,
	})
	if err != nil {
		return ElementSelection{}, err
	}
	if result == nil {
		return ElementSelection{}, fmt.Errorf("no inspectable element at point")
	}
	return evaluateResultToSelection(result)
}

func inspectSelectionByDirection(page playwright.Page, tab Tab, selector string, direction string) (ElementSelection, error) {
	result, err := page.Evaluate(`(input) => {
		const eventTypes = [
			'click', 'dblclick', 'mousedown', 'mouseup', 'mouseover', 'mousemove', 'mouseout',
			'keydown', 'keyup', 'keypress', 'focus', 'blur', 'change', 'input', 'submit',
			'scroll', 'resize', 'load', 'error', 'touchstart', 'touchend', 'touchmove',
		];
		const meaningfulAttrs = new Set([
			'id', 'role', 'type', 'name', 'value', 'href', 'src', 'alt', 'title',
			'placeholder', 'disabled', 'readonly', 'required', 'checked', 'selected',
			'aria-label', 'aria-labelledby', 'aria-describedby', 'aria-expanded',
			'aria-selected', 'aria-checked', 'aria-hidden', 'aria-disabled',
			'data-testid', 'data-id', 'for', 'action', 'method', 'target', 'rel',
			'download', 'pattern', 'min', 'max', 'step', 'maxlength'
		]);
		const resolveTargetElement = (raw) => {
			if (!raw || raw.nodeType !== 1) return null;
			let el = raw;
			while (el) {
				const cs = window.getComputedStyle(el);
				if (cs.display === 'none' || cs.visibility === 'hidden' || parseFloat(cs.opacity || '1') === 0) {
					el = el.parentElement;
					continue;
				}
				if (cs.pointerEvents === 'none' && el !== document.body && el !== document.documentElement) {
					el = el.parentElement;
					continue;
				}
				const rect = el.getBoundingClientRect();
				if (rect.width > 0 && rect.height > 0) return el;
				el = el.parentElement;
			}
			return null;
		};
		const cssEscape = (value) => {
			try { return CSS.escape(value); } catch { return value; }
		};
		const buildSelectorPath = (el) => {
			const parts = [];
			let current = el;
			while (current && parts.length < 5) {
				let part = current.tagName.toLowerCase();
				if (current.id) {
					part += '#' + current.id;
					parts.unshift(part);
					break;
				}
				const cls = (current.getAttribute('class') || '').trim().split(/\s+/).filter(Boolean).slice(0, 2).join('.');
				if (cls) part += '.' + cls;
				parts.unshift(part);
				current = current.parentElement;
			}
			return parts.join(' > ');
		};
		const buildUniqueSelector = (el) => {
			const parts = [];
			let current = el;
			let depth = 0;
			while (current && current !== document.documentElement && depth < 8) {
				let part = current.tagName.toLowerCase();
				if (current.id) {
					part += '#' + cssEscape(current.id);
					parts.unshift(part);
					break;
				}
				const parent = current.parentElement;
				if (parent) {
					const siblings = Array.from(parent.children);
					const sameTag = siblings.filter((entry) => entry.tagName === current.tagName);
					if (sameTag.length > 1) {
						const idx = sameTag.indexOf(current) + 1;
						part += ':nth-child(' + idx + ')';
					}
				}
				const cls = (current.getAttribute('class') || '').trim().split(/\s+/).filter(Boolean).slice(0, 2);
				if (cls.length > 0) {
					part += cls.map((name) => '.' + cssEscape(name)).join('');
				}
				parts.unshift(part);
				current = parent;
				depth++;
			}
			if (current === document.documentElement) parts.unshift('html');
			return parts.join(' > ');
		};
		const buildParentChain = (el) => {
			const chain = [];
			let current = el;
			let depth = 0;
			while (current && current !== document.documentElement && depth < 6) {
				chain.unshift({
					tagName: current.tagName.toLowerCase(),
					id: current.id || undefined,
					classes: (current.getAttribute('class') || '').trim().split(/\s+/).filter(Boolean),
				});
				current = current.parentElement;
				depth++;
			}
			return chain;
		};
		const extractBoxModel = (el) => {
			const cs = window.getComputedStyle(el);
			const rect = el.getBoundingClientRect();
			const parse = (value) => parseFloat(value) || 0;
			const mt = parse(cs.marginTop), mr = parse(cs.marginRight), mb = parse(cs.marginBottom), ml = parse(cs.marginLeft);
			const bt = parse(cs.borderTopWidth), br = parse(cs.borderRightWidth), bb = parse(cs.borderBottomWidth), bl = parse(cs.borderLeftWidth);
			const pt = parse(cs.paddingTop), pr = parse(cs.paddingRight), pb = parse(cs.paddingBottom), pl = parse(cs.paddingLeft);
			const contentWidth = Math.max(0, rect.width - bl - br - pl - pr);
			const contentHeight = Math.max(0, rect.height - bt - bb - pt - pb);
			return {
				margin: { top: mt, right: mr, bottom: mb, left: ml },
				border: { top: bt, right: br, bottom: bb, left: bl },
				padding: { top: pt, right: pr, bottom: pb, left: pl },
				contentRect: {
					x: rect.x + bl + pl,
					y: rect.y + bt + pt,
					width: contentWidth,
					height: contentHeight,
				},
			};
		};
		const extractAccessibilityInfo = (el) => {
			const cs = window.getComputedStyle(el);
			const role = el.getAttribute('role') || undefined;
			const ariaLabel = el.getAttribute('aria-label') || el.getAttribute('aria-labelledby') || undefined;
			const tabIndexAttr = el.getAttribute('tabindex');
			const tabIndex = tabIndexAttr !== null ? parseInt(tabIndexAttr, 10) : undefined;
			const focusableTags = new Set(['a', 'button', 'input', 'select', 'textarea', 'details', 'summary']);
			const focusable = focusableTags.has(el.tagName.toLowerCase()) || (tabIndexAttr !== null && (tabIndex || -1) >= 0);
			let contrastRatio;
			if (cs.color && cs.backgroundColor && cs.backgroundColor !== 'rgba(0, 0, 0, 0)' && cs.backgroundColor !== 'transparent') {
				contrastRatio = cs.color + ' on ' + cs.backgroundColor;
			}
			return {
				role,
				label: ariaLabel,
				tabIndex,
				focusable,
				contrastRatio,
			};
		};
		const extractEventListeners = (el) => {
			const listeners = [];
			for (const type of eventTypes) {
				const attr = el.getAttribute('on' + type);
				if (attr) listeners.push({ type, handlerBody: attr.slice(0, 200) });
			}
			return listeners;
		};
		const findVisibleChild = (el) => {
			for (const child of Array.from(el.children)) {
				const resolved = resolveTargetElement(child);
				if (resolved) return resolved;
			}
			return null;
		};
		const findVisibleSibling = (el, key) => {
			let sibling = el[key];
			while (sibling) {
				const resolved = resolveTargetElement(sibling);
				if (resolved) return resolved;
				sibling = sibling[key];
			}
			return null;
		};
		let element = resolveTargetElement(document.querySelector(input.selector));
		if (!element) return null;
		switch (input.direction) {
			case 'up':
				element = resolveTargetElement(element.parentElement) || element;
				break;
			case 'down':
				element = findVisibleChild(element) || element;
				break;
			case 'right':
				element = findVisibleSibling(element, 'nextElementSibling') || element;
				break;
			case 'left':
				element = findVisibleSibling(element, 'previousElementSibling') || element;
				break;
			default:
				break;
		}
		const attrs = {};
		for (const attr of Array.from(element.attributes)) {
			if (meaningfulAttrs.has(attr.name) || (attr.name.startsWith('data-') && attr.name !== 'data-nine-proxy-runtime')) {
				attrs[attr.name] = attr.value;
			}
		}
		const rect = element.getBoundingClientRect();
		const cs = window.getComputedStyle(element);
		const text = (element.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 600);
		const computedStyle = {
			display: cs.display,
			position: cs.position,
			width: cs.width,
			height: cs.height,
			color: cs.color,
			backgroundColor: cs.backgroundColor,
			fontSize: cs.fontSize,
			fontFamily: cs.fontFamily,
			fontWeight: cs.fontWeight,
			lineHeight: cs.lineHeight,
			textAlign: cs.textAlign,
			margin: cs.margin,
			padding: cs.padding,
			border: cs.border,
			borderRadius: cs.borderRadius,
			overflow: cs.overflow,
			opacity: cs.opacity,
			visibility: cs.visibility,
			zIndex: cs.zIndex,
		};
		if (cs.display.includes('flex')) computedStyle.flex = cs.flex;
		if (cs.display.includes('grid')) computedStyle.grid = cs.gridTemplateColumns;
		if (cs.gap !== 'normal') computedStyle.gap = cs.gap;
		if (cs.position !== 'static') {
			computedStyle.top = cs.top;
			computedStyle.left = cs.left;
			computedStyle.right = cs.right;
			computedStyle.bottom = cs.bottom;
		}
		return {
			tabId: input.tabId,
			url: input.url,
			title: input.title,
			tagName: element.tagName,
			role: element.getAttribute('role') || undefined,
			text: text || undefined,
			selector: buildSelectorPath(element),
			uniqueSelector: buildUniqueSelector(element),
			outerHTML: element.outerHTML.slice(0, 4000),
			attributes: Object.keys(attrs).length > 0 ? attrs : undefined,
			computedStyle,
			boundingRect: {
				width: rect.width,
				height: rect.height,
				x: rect.x,
				y: rect.y,
			},
			boxModel: extractBoxModel(element),
			parentChain: buildParentChain(element),
			accessibilityInfo: extractAccessibilityInfo(element),
			eventListeners: extractEventListeners(element),
		};
	}`, map[string]any{
		"selector":  selector,
		"direction": direction,
		"tabId":     tab.ID,
		"url":       tab.URL,
		"title":     tab.Title,
	})
	if err != nil {
		return ElementSelection{}, err
	}
	if result == nil {
		return ElementSelection{}, fmt.Errorf("no inspectable element for selector")
	}
	return evaluateResultToSelection(result)
}

func evaluateResultToSelection(result any) (ElementSelection, error) {
	if result == nil {
		return ElementSelection{}, fmt.Errorf("empty inspect result")
	}
	data, err := json.Marshal(result)
	if err != nil {
		return ElementSelection{}, err
	}
	var selection ElementSelection
	if err := json.Unmarshal(data, &selection); err != nil {
		return ElementSelection{}, err
	}
	selection.TagName = strings.TrimSpace(selection.TagName)
	if selection.TagName == "" {
		return ElementSelection{}, fmt.Errorf("inspect result missing tagName")
	}
	return selection, nil
}
