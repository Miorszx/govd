package libav

// AAC profile selection for delivery.
//
// Why this exists: the delivered track has to play on the widest set of
// receivers Telegram forwards to, and those disagree.
//
//   AAC-LC   decodes everywhere, including hardware decoders that predate HE.
//   AAC-HE   (HE-AAC v1 / SBR) is what iOS uses for WhatsApp voice and video
//            notes, and what Facebook sources carry in the first place.
//            It gets ~2x the perceived quality at half the file size, but the
//            lowest rungs of decoders do not understand the SBR extension and
//            play it as if it were LC, which is audible as a dull, hollow
//            track.
//
// Measured 2026-11-24 on a Facebook share post: source is HE-AAC stereo
// 44100 Hz at 48 kbps. Re-encoding it to LC at 128k spent 2.7x the bits
// inventing detail the source never carried, and re-encoding to LC at 48k
// threw away what the source did carry.
//
// The profile is therefore an operator choice, not a hardcoded answer, and it
// defaults to HE so an iPhone forwards the file with its audio intact.

import (
	"fmt"
	"os"
	"strings"
)

// AAC profile names as ffmpeg spells them.
const (
	AACProfileLC  = "aac_low"   // AAC-LC: universal, no SBR
	AACProfileHE  = "aac_he"    // HE-AAC v1: SBR, what iOS/WhatsApp expect
	AACProfileHE2 = "aac_he_v2" // HE-AAC v2: SBR + PS
)

// CanEncode reports whether this ffmpeg build can produce the profile.
//
// The native "aac" encoder in ffmpeg (and in the Alpine build this bot
// ships inside) supports exactly one profile: aac_low. Asking it for aac_he
// fails at encoder-open with "Profile not supported!", measured 2026-11-24
// on ffmpeg 8.0.1 and again inside the container:
//
//	aac_he     32k/48k/64k  FAIL  Profile not supported!
//	aac_he_v2  32k/48k/64k  FAIL  Profile not supported!
//	aac_low    32k/48k/64k  OK
//
// Only libfdk_aac adds HE, and this build does not carry it. So an HE target
// can never be met by re-encoding here -- it is met by NOT re-encoding, which
// is what the caller does when the source is already HE.

// EncodeSupported reports whether target can be produced by this build.
func EncodeSupported(target string) bool {
	return target == AACProfileLC
}

// TargetAACProfile returns the profile an operator wants on delivery.
//
// AAC_PROFILE env overrides; unset means HE, because the sources this bot sees
// are HE and the receivers that matter are phones.
func TargetAACProfile() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AAC_PROFILE"))) {
	case "lc", "aac_lc", "aac-lc":
		return AACProfileLC
	case "he2", "he_v2", "heaacv2", "aac_he_v2":
		return AACProfileHE2
	case "", "he", "heaac", "aac_he", "aac-he":
		return AACProfileHE
	default:
		return AACProfileHE
	}
}

// aacBitrateFor picks a bitrate for the requested profile.
//
// HE needs roughly half the bits of LC for the same perceived quality because
// SBR reconstructs the top octave. Asking for LC-level bitrates on HE wastes
// space; asking for HE-level bitrates on LC sounds thin.
func aacBitrateFor(profile string, sourceBitrate int) int {
	if sourceBitrate <= 0 || sourceBitrate > 320000 {
		sourceBitrate = 64000
	}
	switch profile {
	case AACProfileHE:
		// HE at the source's own rate is already generous; floor at 48k so a
		// quiet source is not starved, ceiling at 128k so HE's efficiency is
		// not squandered re-encoding detail that was never there.
		b := sourceBitrate
		if b < 48000 {
			b = 48000
		}
		if b > 128000 {
			b = 128000
		}
		return b
	case AACProfileHE2:
		// HE v2 folds stereo into one channel plus PS, so it needs less.
		b := sourceBitrate
		if b < 32000 {
			b = 32000
		}
		if b > 64000 {
			b = 64000
		}
		return b
	default: // LC
		b := sourceBitrate * 2 // LC needs roughly double for the same quality
		if b < 96000 {
			b = 96000
		}
		if b > 192000 {
			b = 192000
		}
		return b
	}
}

// ProfileMatches reports whether an existing track already satisfies target.
//
// ffprobe reports HE tracks as profile "HE-AAC" or "HE-AACv2" and LC tracks as
// "LC". A LC source asked to stay LC needs no work at all, which is the case
// that keeps a re-encode off the hot path.
// ProfileMatches reports whether an existing track already satisfies target.
func ProfileMatches(probeProfile, target string) bool {
	p := strings.ToLower(strings.TrimSpace(probeProfile))
	switch target {
	case AACProfileLC:
		return p == "lc" || p == "aac lc"
	case AACProfileHE:
		return strings.HasPrefix(p, "he-aac") && !strings.Contains(p, "v2")
	case AACProfileHE2:
		return strings.Contains(p, "he-aacv2") || strings.Contains(p, "he-aac v2")
	}
	return false
}

// describeProfile is for log lines: it names what will happen and why.
func describeProfile(probeProfile, target string, bitrate int) string {
	if ProfileMatches(probeProfile, target) {
		return fmt.Sprintf("already %s profile=%q - no re-encode", target, probeProfile)
	}
	return fmt.Sprintf("profile=%q -> %s (%dk) for phone playback",
		probeProfile, target, bitrate/1000)
}
