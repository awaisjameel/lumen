package core

import "testing"

func TestPlaybackOf(t *testing.T) {
	for _, tc := range []struct {
		ext, video, audio string
		want              Playback
	}{
		{".mp4", "h264", "aac", PlayDirect},
		{".mov", "hevc", "aac", PlayHEVC},
		{".webm", "vp9", "opus", PlayDirect},
		{".mp4", "h264", "ac3", PlayConvert},
		{".mkv", "h264", "aac", PlayConvert},
		{".avi", "mpeg4", "mp3", PlayConvert},
	} {
		if got := PlaybackOf(tc.ext, tc.video, tc.audio); got != tc.want {
			t.Errorf("%s %s %s: %s, want %s", tc.ext, tc.video, tc.audio, got, tc.want)
		}
	}
}
