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

package stream

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/trace"

	"github.com/rusq/slack"

	"github.com/rusq/slackdump/v4/processor"
)

func (cs *Stream) channelWorker(ctx context.Context, proc processor.Conversations, results chan<- Result, threadC chan<- request, reqs <-chan request) {
	ctx, task := trace.NewTask(ctx, "channelWorker")
	defer task.End()

	for {
		select {
		case <-ctx.Done():
			results <- Result{Type: RTChannel, Err: ctx.Err()}
			return
		case req, more := <-reqs:
			if !more {
				return // channel closed
			}
			channel, err := cs.procChannelInfoWithUsersAndCanvases(ctx, proc, req.sl.Channel, req.sl.ThreadTS)
			if err != nil {
				results <- Result{Type: RTChannel, ChannelID: req.sl.Channel, Err: err}
				continue
			}

			if err := cs.channel(ctx, req, func(mm []slack.Message, isLast bool) error {
				n, err := cs.procChanMsg(ctx, proc, threadC, channel, isLast, mm)
				if err != nil {
					return err
				}
				results <- Result{Type: RTChannel, ChannelID: req.sl.Channel, ThreadCount: n, IsLast: isLast}
				return nil
			}); err != nil {
				results <- Result{Type: RTChannel, ChannelID: req.sl.Channel, Err: err}
				continue
			}
		}
	}
}

func (cs *Stream) threadWorker(ctx context.Context, proc processor.Conversations, results chan<- Result, threadReq <-chan request) {
	ctx, task := trace.NewTask(ctx, "threadWorker")
	defer task.End()

	for {
		select {
		case <-ctx.Done():
			results <- Result{Type: RTThread, Err: ctx.Err()}
			return
		case req, more := <-threadReq:
			if !more {
				return // channel closed
			}
			if !req.sl.IsThread() {
				results <- Result{Type: RTThread, Err: fmt.Errorf("invalid thread link: %s", req.sl)}
				continue
			}

			channel := new(slack.Channel)
			if req.threadOnly {
				// Thread-only requests come from direct thread links (e.g., resume).
				// We only need channel info (ID, name, etc.) for file paths and
				// identification. Channel users are already recorded from the
				// original channel archive, and thread messages contain their own
				// user IDs. Skipping procChannelUsers saves an API call per thread.
				var err error
				if channel, err = cs.procChannelInfo(ctx, proc, req.sl.Channel, req.sl.ThreadTS); err != nil {
					results <- Result{Type: RTThread, ChannelID: req.sl.Channel, ThreadTS: req.sl.ThreadTS, Err: err}
					continue
				}
			} else {
				// hackety hack
				// Threads discovered from channel messages. The channel info was
				// already fetched by channelWorker, so we just need the ID.
				channel.ID = req.sl.Channel
			}
			if err := cs.thread(ctx, req, func(msgs []slack.Message, isLast bool) error {
				if err := procThreadMsg(ctx, proc, channel, req.sl.ThreadTS, req.threadOnly, isLast, msgs); err != nil {
					return err
				}
				results <- Result{Type: RTThread, ChannelID: req.sl.Channel, ThreadTS: req.sl.ThreadTS, IsLast: isLast}
				return nil
			}); err != nil {
				results <- Result{Type: RTThread, ChannelID: req.sl.Channel, ThreadTS: req.sl.ThreadTS, Err: err}
				continue
			}
		}
	}
}

func (cs *Stream) channelInfoWorker(ctx context.Context, proc processor.ChannelInformer, srC chan<- Result, channelIdC <-chan string) {
	ctx, task := trace.NewTask(ctx, "channelInfoWorker")
	defer task.End()

	infoFetcher := cs.procChannelInfoWithUsers
	if cs.fastSearch {
		infoFetcher = cs.procChannelInfo
	}

	seen := make(map[string]struct{}, 512)

	for {
		select {
		case <-ctx.Done():
			return
		case id, more := <-channelIdC:
			if !more {
				return
			}
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}

			if _, err := infoFetcher(ctx, proc, id, ""); err != nil {
				// if _, err := cs.procChannelInfo(ctx, proc, id, ""); err != nil {
				srC <- Result{Type: RTChannelInfo, ChannelID: id, Err: fmt.Errorf("channelInfoWorker: %s: %w", id, err)}
			}
			seen[id] = struct{}{}
		}
	}
}

// procChannelInfoWithUsersAndCanvases fetches the channel info and member
// list, enumerates any canvases attached to the channel (primary + tabs), and
// persists everything in the correct order: canvas file IDs are patched onto
// the channel record before proc.ChannelInfo is called, so the recorded
// channel info reflects the canvas content. Canvas files themselves are then
// pushed through proc.Files.
//
// The patching is the reason this function exists rather than the simpler
// procChannelInfoWithUsers + post-hoc canvas dump pair: channel info chunks
// are read first-write-wins, so the FileId mutation must happen before the
// first proc.ChannelInfo call.
func (cs *Stream) procChannelInfoWithUsersAndCanvases(ctx context.Context, proc processor.Conversations, channelID, threadTS string) (*slack.Channel, error) {
	info, err := cs.channelInfo(ctx, channelID, threadTS)
	if err != nil {
		return nil, err
	}

	members, err := cs.procChannelUsers(ctx, proc, channelID, threadTS)
	if err != nil {
		return nil, err
	}
	info.Members = members

	canvasFiles := cs.discoverCanvases(ctx, info)

	// Patch the channel record so the viewer can locate the canvas(es) via
	// the channel Properties. The viewer enumerates canvas tabs from
	// Properties.Tabs[] and renders one button per entry, so we rewrite the
	// tab entries to encode the discovered file IDs. Canvas.FileId is kept
	// in sync (points at the first canvas) for any legacy code that still
	// reads it.
	if info.Properties != nil && len(canvasFiles) > 0 {
		if info.Properties.Canvas.FileId == "" {
			info.Properties.Canvas.FileId = canvasFiles[0].ID
		}
		info.Properties.Tabs = rebuildCanvasTabs(info.Properties.Tabs, canvasFiles)
	}

	if err := proc.ChannelInfo(ctx, info, threadTS); err != nil {
		return nil, err
	}

	for _, f := range canvasFiles {
		if err := proc.Files(ctx, info, slack.Message{}, []slack.File{f}); err != nil {
			slog.Warn("canvas file processor error", "channel", info.ID, "file", f.ID, "err", err)
		}
	}

	return info, nil
}

// discoverCanvases returns the canvas files attached to a channel. It merges
// the primary canvas (properties.canvas.file_id) with any canvases attached
// as channel tabs (resolved via files.list?types=canvases). Errors are
// logged and swallowed — canvas retrieval is best effort.
func (cs *Stream) discoverCanvases(ctx context.Context, channel *slack.Channel) []slack.File {
	if channel.Properties == nil {
		return nil
	}
	props := channel.Properties

	seen := make(map[string]struct{})
	var out []slack.File

	if fid := props.Canvas.FileId; fid != "" {
		seen[fid] = struct{}{}
		if f, err := cs.canvasFile(ctx, fid); err != nil {
			slog.Warn("canvas error", "channel", channel.ID, "err", err)
		} else if f != nil {
			out = append(out, *f)
		}
	} else if !props.Canvas.IsEmpty {
		slog.Warn("canvas reported non-empty but file_id is missing", "channel", channel.ID)
	}

	var hasCanvasTab bool
	for _, tab := range props.Tabs {
		if tab.Type == "canvas" {
			hasCanvasTab = true
			break
		}
	}
	if !hasCanvasTab {
		return out
	}

	files, err := cs.listCanvases(ctx, channel.ID)
	if err != nil {
		slog.Warn("canvas tab listing failed", "channel", channel.ID, "err", err)
		return out
	}
	for _, f := range files {
		if _, ok := seen[f.ID]; ok {
			continue
		}
		seen[f.ID] = struct{}{}
		full, err := cs.canvasFile(ctx, f.ID)
		if err != nil {
			slog.Warn("canvas tab error", "channel", channel.ID, "file", f.ID, "err", err)
			continue
		}
		if full != nil {
			out = append(out, *full)
		}
	}
	return out
}

// rebuildCanvasTabs replaces the canvas entries in the channel's tab list
// with one entry per discovered canvas file. Each rewritten entry uses the
// file ID (e.g. "F08LR6MPPE1") as Tab.ID — the original Ct… tab IDs are
// opaque and not usable for fetching, while the file ID is what the viewer
// needs to render the canvas. Non-canvas tabs are preserved in their
// original position; canvas tabs are appended at the end. The first entry
// with a non-empty Label keeps that label; otherwise the canvas file's
// Title is used.
func rebuildCanvasTabs(tabs []slack.Tab, files []slack.File) []slack.Tab {
	out := make([]slack.Tab, 0, len(tabs)+len(files))
	var existingLabels []string
	for _, t := range tabs {
		if t.Type == "canvas" {
			if t.Label != "" {
				existingLabels = append(existingLabels, t.Label)
			}
			continue
		}
		out = append(out, t)
	}
	for i, f := range files {
		label := f.Title
		if label == "" && i < len(existingLabels) {
			label = existingLabels[i]
		}
		if label == "" {
			label = fmt.Sprintf("Canvas %d", i+1)
		}
		out = append(out, slack.Tab{
			ID:    f.ID,
			Label: label,
			Type:  "canvas",
		})
	}
	return out
}

// canvasFile resolves a canvas file by ID via files.info. Returns nil if
// fileId is empty or the file cannot be found.
func (cs *Stream) canvasFile(ctx context.Context, fileId string) (*slack.File, error) {
	if fileId == "" {
		return nil, nil
	}
	file, _, _, err := cs.client.GetFileInfoContext(ctx, fileId, 0, 1)
	if err != nil {
		return nil, fmt.Errorf("canvas: %s: %w", fileId, err)
	}
	if file == nil {
		return nil, errors.New("canvas: file not found")
	}
	return file, nil
}

// listCanvases enumerates canvas files attached to the channel via files.list.
// Returns all pages flattened.
func (cs *Stream) listCanvases(ctx context.Context, channelID string) ([]slack.File, error) {
	params := slack.GetFilesParameters{
		Channel: channelID,
		Types:   "canvases",
		Count:   100,
		Page:    1,
	}
	var out []slack.File
	for {
		files, paging, err := cs.client.GetFilesContext(ctx, params)
		if err != nil {
			return nil, err
		}
		out = append(out, files...)
		if paging == nil || paging.Page >= paging.Pages || len(files) == 0 {
			break
		}
		params.Page = paging.Page + 1
	}
	return out, nil
}

