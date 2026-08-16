package constant

import "strings"

type TaskPlatform string

const (
	TaskPlatformSuno       TaskPlatform = "suno"
	TaskPlatformMidjourney TaskPlatform = "mj"
	TaskPlatformSeedance   TaskPlatform = "61"
)

const (
	SunoActionMusic  = "MUSIC"
	SunoActionLyrics = "LYRICS"

	TaskActionGenerate          = "generate"
	TaskActionTextGenerate      = "textGenerate"
	TaskActionFirstTailGenerate = "firstTailGenerate"
	TaskActionReferenceGenerate = "referenceGenerate"
	TaskActionRemix             = "remixGenerate"
)

var SunoModel2Action = map[string]string{
	"suno_music":  SunoActionMusic,
	"suno_lyrics": SunoActionLyrics,
}

// IsSeedanceVideoRequestPath keeps the video-only Seedance channel out of
// chat, image, audio, and other relay selection paths.
func IsSeedanceVideoRequestPath(path string) bool {
	path = strings.TrimSuffix(path, "/")
	return path == "/v1/videos" ||
		strings.HasPrefix(path, "/v1/videos/") ||
		path == "/v1/video/generations" ||
		strings.HasPrefix(path, "/v1/video/generations/") ||
		path == "/v1/midjourney/generations/video" ||
		path == "/pg/video/generations"
}
