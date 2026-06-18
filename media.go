package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

// ---------------------------------------------------------------------------
// Directory constants
// ---------------------------------------------------------------------------

// MediaCacheDir stores all regular (non-view-once) incoming media.
const MediaCacheDir = "media_cache"

// ViewOnceCacheDir stores intercepted view-once / ephemeral media.
const ViewOnceCacheDir = "downloaded_media"

// ---------------------------------------------------------------------------
// Shared data type
// ---------------------------------------------------------------------------

// CachedMedia holds metadata for a single cached media file.
type CachedMedia struct {
	ID        string `json:"id"`
	Sender    string `json:"sender"`
	PushName  string `json:"push_name"`
	Chat      string `json:"chat"`
	MediaType string `json:"media_type"` // image, video, audio, document, sticker
	FileName  string `json:"file_name"`
	FilePath  string `json:"file_path"`
	FileSize  int64  `json:"file_size"`
	Timestamp string `json:"timestamp"`
	IsGroup   bool   `json:"is_group"`
	IsVO      bool   `json:"is_view_once"`
}

// msgMeta bundles immutable per-event metadata to keep signatures short.
type msgMeta struct {
	id, sender, pushName, chat string
	isGroup                    bool
}

var (
	mediaCacheList []CachedMedia
	mediaCacheLock sync.RWMutex

	viewOnceCacheList []CachedMedia
	viewOnceCacheLock sync.RWMutex
)

// ---------------------------------------------------------------------------
// Initialisation
// ---------------------------------------------------------------------------

// InitMediaCache ensures both cache directories exist before any message arrives.
func InitMediaCache() {
	for _, dir := range []string{MediaCacheDir, ViewOnceCacheDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("❌ Failed to create media cache directory %q: %v", dir, err)
		}
	}
	log.Printf("📁 Media cache directories ready: %s | %s", MediaCacheDir, ViewOnceCacheDir)
}

// ---------------------------------------------------------------------------
// Public accessors (newest first)
// ---------------------------------------------------------------------------

func GetCachedMediaList() []CachedMedia {
	mediaCacheLock.RLock()
	defer mediaCacheLock.RUnlock()
	return reverseClone(mediaCacheList)
}

func GetViewOnceCacheList() []CachedMedia {
	viewOnceCacheLock.RLock()
	defer viewOnceCacheLock.RUnlock()
	return reverseClone(viewOnceCacheList)
}

func reverseClone(src []CachedMedia) []CachedMedia {
	out := make([]CachedMedia, len(src))
	copy(out, src)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// ---------------------------------------------------------------------------
// STAGE 1 — Entry point wired into handler.go
// ---------------------------------------------------------------------------

// handleMediaMessage is called for every incoming *events.Message.
// It runs the view-once intercept algorithm first; if no view-once frame is
// found it falls through to regular media caching.
func handleMediaMessage(c *whatsmeow.Client, v *events.Message) {
	if v.Message == nil {
		return
	}

	meta := msgMeta{
		id:       v.Info.ID,
		sender:   v.Info.Sender.String(),
		pushName: v.Info.PushName,
		chat:     v.Info.Chat.String(),
		isGroup:  v.Info.IsGroup,
	}

	// STAGE 2 — run the three-path view-once detection algorithm.
	if interceptViewOnce(c, v, meta) {
		return // ephemeral media handled; skip normal cache path
	}

	// STAGE 3 — regular (non-ephemeral) media caching.
	go cacheRegularMedia(c, v, meta)
}

// ---------------------------------------------------------------------------
// STAGE 2 — Three-path view-once detection & extraction algorithm
// ---------------------------------------------------------------------------

// interceptViewOnce checks all three detection paths described in the algorithm
// and, if view-once media is found, downloads and archives it immediately.
//
//	PATH A — ViewOnceMessageV2  (newer protobuf wrapper, FutureProofMessage)
//	PATH B — ViewOnceMessage    (legacy V1 protobuf wrapper)
//	PATH C — ViewOnce bool flag on bare ImageMessage / VideoMessage
//
// Returns true if a view-once frame was detected (regardless of download outcome).
func interceptViewOnce(c *whatsmeow.Client, v *events.Message, meta msgMeta) bool {

	// ── Initialise algorithm state ──────────────────────────────────────────
	isViewOnce := false
	var dl whatsmeow.DownloadableMessage
	ext := ""
	mediaLabel := ""

	// Start with the top-level message; may be replaced by an unpacked inner one.
	workMsg := v.Message

	// ── PATH A: ViewOnceMessageV2 ─────────────────────────────────────────────
	// GetViewOnceMessageV2() returns *waE2E.FutureProofMessage
	if v2 := workMsg.GetViewOnceMessageV2(); v2 != nil {
		if inner := v2.GetMessage(); inner != nil {
			isViewOnce = true
			workMsg = inner // unpack → work on the inner core message
		}
	}

	// ── PATH B: ViewOnceMessage (V1) ─────────────────────────────────────────
	// Only run if PATH A did not already identify a view-once frame.
	if !isViewOnce {
		if v1 := workMsg.GetViewOnceMessage(); v1 != nil {
			if inner := v1.GetMessage(); inner != nil {
				isViewOnce = true
				workMsg = inner // unpack → work on the inner core message
			}
		}
	}

	// ── Extract the media pointer from the (possibly unwrapped) message ───────
	if img := workMsg.GetImageMessage(); img != nil {
		// PATH C: per-message ViewOnce boolean flag
		if img.GetViewOnce() {
			isViewOnce = true
		}
		dl = img
		ext = mimeToExt(img.GetMimetype(), ".jpg")
		mediaLabel = "image"

	} else if vid := workMsg.GetVideoMessage(); vid != nil {
		// PATH C: per-message ViewOnce boolean flag
		if vid.GetViewOnce() {
			isViewOnce = true
		}
		dl = vid
		ext = mimeToExt(vid.GetMimetype(), ".mp4")
		mediaLabel = "video"
	}

	// ── STAGE 3: Download & Storage ───────────────────────────────────────────
	if isViewOnce && dl != nil {
		fmt.Printf("🕵️  Ephemeral view-once media detected from: %s\n", meta.sender)
		go downloadViewOnce(c, dl, ext, mediaLabel, meta)
		return true
	}

	return false
}

// downloadViewOnce downloads and permanently archives a view-once media file.
func downloadViewOnce(c *whatsmeow.Client, dl whatsmeow.DownloadableMessage, ext, mediaLabel string, meta msgMeta) {
	// Invoke whatsmeow's internal media downloader transport layers
	rawBytes, downloadError := c.Download(context.Background(), dl)
	if downloadError != nil {
		log.Printf("❌ Extraction error fetching payload from WhatsApp CDN: %v", downloadError)
		return
	}

	// Construct a unique local file name: view_once_<msgID>.<ext>
	fileName := fmt.Sprintf("view_once_%s%s", meta.id, ext)
	savePath := filepath.Join(ViewOnceCacheDir, fileName)

	// Write raw unencrypted bytes permanently to storage disk
	if saveError := os.WriteFile(savePath, rawBytes, 0666); saveError != nil {
		log.Printf("❌ Failed writing payload buffer to system directory: %v", saveError)
		return
	}

	fmt.Printf("✅ Vanishing file successfully archived to: %s\n", savePath)

	// Register in the view-once intercept list (for dashboard display)
	entry := CachedMedia{
		ID:        meta.id,
		Sender:    meta.sender,
		PushName:  meta.pushName,
		Chat:      meta.chat,
		MediaType: mediaLabel,
		FileName:  fileName,
		FilePath:  savePath,
		FileSize:  int64(len(rawBytes)),
		Timestamp: time.Now().Format("2006-01-02 15:04:05"),
		IsGroup:   meta.isGroup,
		IsVO:      true,
	}

	viewOnceCacheLock.Lock()
	viewOnceCacheList = append(viewOnceCacheList, entry)
	viewOnceCacheLock.Unlock()
}

// ---------------------------------------------------------------------------
// Regular (non-view-once) media caching
// ---------------------------------------------------------------------------

// cacheRegularMedia downloads and saves all normal (non-ephemeral) media types.
func cacheRegularMedia(c *whatsmeow.Client, v *events.Message, meta msgMeta) {
	msg := v.Message
	if msg == nil {
		return
	}

	var (
		dl        whatsmeow.DownloadableMessage
		mediaType string
		ext       string
	)

	switch {
	case msg.ImageMessage != nil:
		if msg.ImageMessage.GetViewOnce() {
			return // already handled by interceptViewOnce
		}
		dl = msg.ImageMessage
		mediaType = "image"
		ext = mimeToExt(msg.ImageMessage.GetMimetype(), ".jpg")

	case msg.VideoMessage != nil:
		if msg.VideoMessage.GetViewOnce() {
			return
		}
		dl = msg.VideoMessage
		mediaType = "video"
		ext = mimeToExt(msg.VideoMessage.GetMimetype(), ".mp4")

	case msg.AudioMessage != nil:
		dl = msg.AudioMessage
		mediaType = "audio"
		ext = mimeToExt(msg.AudioMessage.GetMimetype(), ".ogg")

	case msg.DocumentMessage != nil:
		dl = msg.DocumentMessage
		mediaType = "document"
		origExt := filepath.Ext(msg.DocumentMessage.GetFileName())
		if origExt != "" {
			ext = origExt
		} else {
			ext = mimeToExt(msg.DocumentMessage.GetMimetype(), ".bin")
		}

	case msg.StickerMessage != nil:
		dl = msg.StickerMessage
		mediaType = "sticker"
		ext = ".webp"

	default:
		return // no cacheable media in this message
	}

	if dl == nil {
		return
	}

	data, err := c.Download(context.Background(), dl)
	if err != nil {
		log.Printf("⚠️ Failed to download %s from %s (id=%s): %v", mediaType, meta.sender, meta.id, err)
		return
	}

	ts := time.Now()
	shortID := meta.id
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	fileName := fmt.Sprintf("%s_%s_%s%s", mediaType, ts.Format("20060102_150405"), shortID, ext)
	filePath := filepath.Join(MediaCacheDir, fileName)

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		log.Printf("❌ Failed to write cached media to %s: %v", filePath, err)
		return
	}

	entry := CachedMedia{
		ID:        meta.id,
		Sender:    meta.sender,
		PushName:  meta.pushName,
		Chat:      meta.chat,
		MediaType: mediaType,
		FileName:  fileName,
		FilePath:  filePath,
		FileSize:  int64(len(data)),
		Timestamp: ts.Format("2006-01-02 15:04:05"),
		IsGroup:   meta.isGroup,
		IsVO:      false,
	}

	mediaCacheLock.Lock()
	mediaCacheList = append(mediaCacheList, entry)
	mediaCacheLock.Unlock()

	log.Printf("💾 Cached %s from %s → %s (%d bytes)", mediaType, meta.sender, fileName, len(data))
}

// ---------------------------------------------------------------------------
// MIME → extension helper
// ---------------------------------------------------------------------------

func mimeToExt(mime, def string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/3gpp":
		return ".3gp"
	case "audio/ogg", "audio/ogg; codecs=opus":
		return ".ogg"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/aac":
		return ".aac"
	case "application/pdf":
		return ".pdf"
	case "application/zip":
		return ".zip"
	}
	return def
}
