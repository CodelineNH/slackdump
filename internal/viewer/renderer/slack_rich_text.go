// Copyright (c) 2021-2026 Rustam Gilyazov and Contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package renderer

import (
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"strings"

	emj "github.com/enescakir/emoji"
	"github.com/rusq/slack"
)

func (s *Slack) mbtRichText(ib slack.Block) (string, string, error) {
	b, ok := ib.(*slack.RichTextBlock)
	if !ok {
		return "", "", NewErrIncorrectType(&slack.RichTextBlock{}, ib)
	}
	var buf, cbuf strings.Builder
	for _, el := range b.Elements {
		fn, ok := rteTypeHandlers[el.RichTextElementType()]
		if !ok {
			return "", "", NewErrMissingHandler(el.RichTextElementType())
		}
		s, cl, err := fn(s, el)
		if err != nil {
			return "", "", err
		}
		buf.WriteString(s)
		cbuf.WriteString(cl)
	}

	return buf.String() + cbuf.String(), "", nil
}

func (s *Slack) rteSection(ie slack.RichTextElement) (string, string, error) {
	e, ok := ie.(*slack.RichTextSection)
	if !ok {
		return "", "", NewErrIncorrectType(&slack.RichTextSection{}, ie)
	}
	var buf strings.Builder
	var cbuf strings.Builder
	for _, el := range e.Elements {
		fn, ok := rtseTypeHandlers[el.RichTextSectionElementType()]
		if !ok {
			return "", "", NewErrMissingHandler(el.RichTextSectionElementType())
		}
		s, cl, err := fn(s, el)
		if err != nil {
			return "", "", err
		}
		buf.WriteString(s)
		cbuf.WriteString(cl)
	}

	return buf.String() + cbuf.String(), "", nil
}

func (s *Slack) rtseText(ie slack.RichTextSectionElement) (string, string, error) {
	e, ok := ie.(*slack.RichTextSectionTextElement)
	if !ok {
		return "", "", NewErrIncorrectType(&slack.RichTextSectionTextElement{}, ie)
	}
	t := strings.ReplaceAll(html.EscapeString(e.Text), "\n", "<br>")

	return applyStyle(t, e.Style), "", nil
}

func applyStyle(s string, style *slack.RichTextSectionTextStyle) string {
	if style == nil {
		return s
	}
	if style.Bold {
		s = fmt.Sprintf("<b>%s</b>", s)
	}
	if style.Italic {
		s = fmt.Sprintf("<i>%s</i>", s)
	}
	if style.Strike {
		s = fmt.Sprintf("<s>%s</s>", s)
	}
	if style.Code {
		s = fmt.Sprintf("<code>%s</code>", s)
	}
	return s
}

func (s *Slack) rtseLink(ie slack.RichTextSectionElement) (string, string, error) {
	e, ok := ie.(*slack.RichTextSectionLinkElement)
	if !ok {
		return "", "", NewErrIncorrectType(&slack.RichTextSectionLinkElement{}, ie)
	}
	if e.Text == "" {
		e.Text = e.URL
	} else {
		e.Text = html.EscapeString(e.Text)
	}
	if s.routes != nil {
		e.URL = s.routes.RewriteSlackURL(e.URL)
	}

	return fmt.Sprintf("<a href=\"%s\">%s</a>", e.URL, e.Text), "", nil
}

func (s *Slack) rteList(ie slack.RichTextElement) (string, string, error) {
	e, ok := ie.(*slack.RichTextList)
	if !ok {
		return "", "", NewErrIncorrectType(&slack.RichTextList{}, ie)
	}
	// const orderedTypes = "1aAiI"
	var tgOpen, tgClose string
	if e.Style == slack.RTEListOrdered {
		// TODO: type alternation on even/odd
		// https://www.w3schools.com/tags/att_ol_type.asp
		tgOpen, tgClose = "<ol>", "</ol>"
	} else {
		tgOpen, tgClose = "<ul>", "</ul>"
	}
	tgOpen, tgClose = strings.Repeat(tgOpen, e.Indent+1), strings.Repeat(tgClose, e.Indent+1)
	var buf, cbuf strings.Builder
	buf.WriteString(tgOpen)
	for _, el := range e.Elements {
		fn, ok := rteTypeHandlers[el.RichTextElementType()]
		if !ok {
			return "", "", NewErrMissingHandler(el.RichTextElementType())
		}
		s, cl, err := fn(s, el)
		if err != nil {
			return "", "", err
		}
		buf.WriteString(fmt.Sprintf("<li>%s</li>", s))
		cbuf.WriteString(cl)
	}
	buf.WriteString(tgClose)
	return buf.String() + cbuf.String(), "", nil
}

func (s *Slack) rteQuote(ie slack.RichTextElement) (string, string, error) {
	e, ok := ie.(*slack.RichTextQuote)
	if !ok {
		return "", "", NewErrIncorrectType(&slack.RichTextQuote{}, ie)
	}
	var buf, cbuf strings.Builder
	buf.WriteString("<blockquote>")
	for _, el := range e.Elements {
		fn, ok := rtseTypeHandlers[el.RichTextSectionElementType()]
		if !ok {
			return "", "", NewErrMissingHandler(el.RichTextSectionElementType())
		}
		s, cl, err := fn(s, el)
		if err != nil {
			return "", "", err
		}
		buf.WriteString(s)
		cbuf.WriteString(cl)
	}
	buf.WriteString(cbuf.String())
	buf.WriteString("</blockquote>")
	return buf.String(), "", nil
}

func (s *Slack) rtePreformatted(ie slack.RichTextElement) (string, string, error) {
	e, ok := ie.(*slack.RichTextPreformatted)
	if !ok {
		return "", "", NewErrIncorrectType(&slack.RichTextPreformatted{}, ie)
	}
	var buf, cbuf strings.Builder
	buf.WriteString("<pre>")
	for _, el := range e.Elements {
		fn, ok := rtseTypeHandlers[el.RichTextSectionElementType()]
		if !ok {
			return "", "", NewErrMissingHandler(el.RichTextSectionElementType())
		}
		s, cl, err := fn(s, el)
		if err != nil {
			return "", "", err
		}
		buf.WriteString(s)
		cbuf.WriteString(cl)
	}
	buf.WriteString(cbuf.String());buf.WriteString("</pre>")
	return buf.String(), "", nil
}

// rtseCanvas renders a "canvas" rich-text-section element. The Slack lib does
// not model canvas refs natively, so they arrive as RichTextSectionUnknownElement
// with a Raw JSON payload containing file_id.
func (s *Slack) rtseCanvas(ie slack.RichTextSectionElement) (string, string, error) {
	e, ok := ie.(*slack.RichTextSectionUnknownElement)
	if !ok {
		return "<i>[canvas]</i>", "", nil
	}
	fileID := extractCanvasFileID(e.Raw)
	if fileID == "" {
		return "<i>[canvas]</i>", "", nil
	}
	escaped := html.EscapeString(fileID)
	if chID := s.canvasChannelOf(fileID); chID != "" && s.routes != nil {
		return fmt.Sprintf(`<a class="slack-canvas-link" href="%s">[canvas: %s]</a>`,
			s.routes.CanvasByFile(chID, fileID), escaped), "", nil
	}
	return fmt.Sprintf("<i>[canvas: %s]</i>", escaped), "", nil
}

// canvasChannelOf searches the channel index for a channel that owns the
// canvas file. The dumper records canvases either on Properties.Canvas.FileId
// (primary) or as Properties.Tabs[].ID entries with Type=="canvas" (tab
// canvases — see rebuildCanvasTabs in the stream package). Returns "" if no
// channel claims the file.
func (s *Slack) canvasChannelOf(fileID string) string {
	if fileID == "" {
		return ""
	}
	for chID, ch := range s.cc {
		if ch.Properties == nil {
			continue
		}
		if ch.Properties.Canvas.FileId == fileID {
			return chID
		}
		for _, t := range ch.Properties.Tabs {
			if t.Type == "canvas" && t.ID == fileID {
				return chID
			}
		}
	}
	return ""
}

func extractCanvasFileID(raw string) string {
	var v struct {
		FileID string `json:"file_id"`
		Raw    string `json:"Raw"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return ""
	}
	if v.FileID != "" {
		return v.FileID
	}
	if v.Raw == "" {
		return ""
	}
	return extractCanvasFileID(v.Raw)
}

func (s *Slack) rtseUser(ie slack.RichTextSectionElement) (string, string, error) {
	e, ok := ie.(*slack.RichTextSectionUserElement)
	if !ok {
		return "", "", NewErrIncorrectType(&slack.RichTextSectionUserElement{}, ie)
	}
	var name string

	if u, ok := s.uu[e.UserID]; s.uu != nil && ok {
		name = u.Name
	} else {
		slog.Warn("user not found", "user_id", e.UserID)
		name = e.UserID
	}

	text := applyStyle(fmt.Sprintf("<@%s>", name), e.Style)
	if s.routes != nil {
		text = fmt.Sprintf(`<a href="%s">%s</a>`, s.routes.User(e.UserID), text)
	}
	return text, "", nil
}

func (s *Slack) rtseEmoji(ie slack.RichTextSectionElement) (string, string, error) {
	e, ok := ie.(*slack.RichTextSectionEmojiElement)
	if !ok {
		return "", "", NewErrIncorrectType(&slack.RichTextSectionEmojiElement{}, ie)
	}
	// TODO: resolve and render emoji.
	em := emj.Parse(fmt.Sprintf(":%s:", e.Name))
	return applyStyle(em, e.Style), "", nil
}

func (s *Slack) rtseChannel(ie slack.RichTextSectionElement) (string, string, error) {
	e, ok := ie.(*slack.RichTextSectionChannelElement)
	if !ok {
		return "", "", NewErrIncorrectType(&slack.RichTextSectionChannelElement{}, ie)
	}
	var name string
	if c, ok := s.cc[e.ChannelID]; s.cc != nil && ok {
		name = c.Name
	} else {
		slog.Warn("channel not found", "channel_id", e.ChannelID)
		name = e.ChannelID
	}

	text := applyStyle(fmt.Sprintf("<#%s>", name), e.Style)
	if s.routes != nil {
		text = fmt.Sprintf(`<a href="%s">%s</a>`, s.routes.Channel(e.ChannelID), text)
	}
	return elDiv(rtseTypeClass[slack.RTSEChannel], text), "", nil
}

func (s *Slack) rtseBroadcast(ie slack.RichTextSectionElement) (string, string, error) {
	e, ok := ie.(*slack.RichTextSectionBroadcastElement)
	if !ok {
		return "", "", NewErrIncorrectType(&slack.RichTextSectionBroadcastElement{}, ie)
	}
	return elStrong(rtseTypeClass[slack.RTSEBroadcast], fmt.Sprintf("@%s ", e.Range)), "", nil
}

func (s *Slack) rtseUserGroup(ie slack.RichTextSectionElement) (string, string, error) {
	e, ok := ie.(*slack.RichTextSectionUserGroupElement)
	if !ok {
		return "", "", NewErrIncorrectType(&slack.RichTextSectionUserGroupElement{}, ie)
	}
	var name string
	if c, ok := s.cc[e.UsergroupID]; s.cc != nil && ok {
		name = c.Name
	} else {
		slog.Warn("channel not found", "usergroup_id", e.UsergroupID)
		name = e.UsergroupID
	}

	return elDiv(rtseTypeClass[slack.RTSEUserGroup], fmt.Sprintf("<@%s>", name)), "", nil
}

func (s *Slack) rtseColor(ie slack.RichTextSectionElement) (string, string, error) {
	e, ok := ie.(*slack.RichTextSectionColorElement)
	if !ok {
		return "", "", NewErrIncorrectType(&slack.RichTextSectionColorElement{}, ie)
	}
	return fmt.Sprintf("<span style=\"color: %s;\">", e.Value), "</span>", nil
}
