package ingestion

import (
	"strings"

	"golang.org/x/net/html"
)

type htmlConverter struct {
	title            string
	markdown         strings.Builder
	sectionHierarchy []string
	titleFound       bool
	listDepth        int
	olCounter        []int
	skipDepth        int
	inCodeBlock      bool
	inPre            bool
}

func htmlToMarkdown(raw string) (title, markdown string, hierarchy []string) {
	c := &htmlConverter{}
	tokenizer := html.NewTokenizer(strings.NewReader(raw))
	for {
		tokenType := tokenizer.Next()
		switch tokenType {
		case html.ErrorToken:
			return c.title, c.markdown.String(), c.sectionHierarchy
		case html.StartTagToken, html.SelfClosingTagToken:
			c.handleOpenTag(tokenizer)
		case html.EndTagToken:
			c.handleCloseTag(tokenizer)
		case html.TextToken:
			c.handleText(tokenizer)
		}
	}
}

func (c *htmlConverter) handleOpenTag(z *html.Tokenizer) {
	name, hasAttrs := z.TagName()
	tagName := string(name)

	if c.skipDepth > 0 {
		if !isVoidElement(tagName) {
			c.skipDepth++
		}
		return
	}

	switch tagName {
	case "script", "style", "nav", "footer", "header", "aside", "noscript":
		if !isVoidElement(tagName) {
			c.skipDepth = 1
		}
		return
	case "pre":
		c.inPre = true
		c.markdown.WriteString("\n```\n")
		return
	case "code":
		if !c.inPre {
			c.markdown.WriteString("`")
		}
		return
	case "br":
		c.markdown.WriteString("\n")
		return
	case "hr":
		c.markdown.WriteString("\n---\n")
		return
	case "img":
		var src, alt string
		if hasAttrs {
			attrs := collectAttrs(z)
			src = attrs["src"]
			alt = attrs["alt"]
		}
		c.markdown.WriteString("![" + alt + "](" + src + ")")
		return
	case "a":
		if hasAttrs {
			attrs := collectAttrs(z)
			c.markdown.WriteString("[")
			c.pushLinkTarget(attrs["href"])
		}
		return
	case "strong", "b":
		c.markdown.WriteString("**")
		return
	case "em", "i":
		c.markdown.WriteString("*")
		return
	case "h1", "h2", "h3", "h4", "h5", "h6":
		level := int(tagName[1] - '0')
		c.markdown.WriteString("\n" + strings.Repeat("#", level) + " ")
		if !c.titleFound && level <= 1 {
			c.titleFound = true
		}
		return
	case "p":
		c.markdown.WriteString("\n\n")
		return
	case "ul":
		c.listDepth++
		return
	case "ol":
		c.listDepth++
		c.olCounter = append(c.olCounter, 1)
		return
	case "li":
		c.markdown.WriteString("\n")
		prefix := "- "
		if len(c.olCounter) > 0 {
			prefix = itoa(c.olCounter[len(c.olCounter)-1]) + ". "
			c.olCounter[len(c.olCounter)-1]++
		}
		c.markdown.WriteString(strings.Repeat("  ", c.listDepth-1) + prefix)
		return
	case "blockquote":
		c.markdown.WriteString("\n> ")
		return
	case "div", "span", "section", "article", "main":
		return
	}
}

func (c *htmlConverter) handleCloseTag(z *html.Tokenizer) {
	name, _ := z.TagName()
	tagName := string(name)

	if c.skipDepth > 0 {
		c.skipDepth--
		return
	}

	switch tagName {
	case "pre":
		c.markdown.WriteString("\n```\n")
		c.inPre = false
		c.inCodeBlock = false
		return
	case "code":
		if !c.inPre {
			c.markdown.WriteString("`")
		}
		return
	case "a":
		c.popLinkTarget()
		return
	case "strong", "b":
		c.markdown.WriteString("**")
		return
	case "em", "i":
		c.markdown.WriteString("*")
		return
	case "h1", "h2", "h3", "h4", "h5", "h6":
		c.markdown.WriteString("\n")
		if c.titleFound && c.title == "" {
			return
		}
		return
	case "p":
		return
	case "ul":
		c.listDepth--
		return
	case "ol":
		c.listDepth--
		if len(c.olCounter) > 0 {
			c.olCounter = c.olCounter[:len(c.olCounter)-1]
		}
		return
	case "li":
		return
	case "blockquote":
		c.markdown.WriteString("\n")
		return
	case "title":
		c.titleFound = true
		return
	case "div", "span", "section", "article", "main", "head", "body":
		return
	}
}

func (c *htmlConverter) handleText(z *html.Tokenizer) {
	if c.skipDepth > 0 {
		return
	}
	text := strings.TrimSpace(string(z.Text()))
	if text == "" {
		return
	}
	c.markdown.WriteString(text)

	if c.titleFound {
		if c.title == "" {
			c.title = text
		}
		c.titleFound = false
	}
}

func (c *htmlConverter) pushLinkTarget(href string) {
	if href == "" {
		return
	}
	c.markdown.WriteString("](" + href + ")")
}

func (c *htmlConverter) popLinkTarget() {
}

func collectAttrs(z *html.Tokenizer) map[string]string {
	attrs := make(map[string]string)
	for {
		key, val, more := z.TagAttr()
		attrs[string(key)] = string(val)
		if !more {
			break
		}
	}
	return attrs
}

func isVoidElement(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input",
		"link", "meta", "param", "source", "track", "wbr":
		return true
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
