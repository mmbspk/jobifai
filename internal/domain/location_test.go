package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatSearchLocation_SeekFromLinkedInStyle(t *testing.T) {
	assert.Equal(t, "All Adelaide SA", FormatSearchLocation("Adelaide, South Australia, Australia", PlatformSeek))
	assert.Equal(t, "All Melbourne VIC", FormatSearchLocation("Melbourne, Victoria, Australia", PlatformSeek))
	assert.Equal(t, "All Sydney NSW", FormatSearchLocation("Sydney, New South Wales, Australia", PlatformSeek))
}

func TestFormatSearchLocation_LinkedInFromSeekStyle(t *testing.T) {
	assert.Equal(t, "Adelaide, South Australia, Australia", FormatSearchLocation("All Adelaide SA", PlatformLinkedIn))
	assert.Equal(t, "Melbourne, Victoria, Australia", FormatSearchLocation("All Melbourne VIC", PlatformLinkedIn))
	assert.Equal(t, "Melbourne, Victoria, Australia", FormatSearchLocation("Melbourne VIC", PlatformLinkedIn))
}

func TestFormatSearchLocation_ShortAliases(t *testing.T) {
	assert.Equal(t, "All Perth WA", FormatSearchLocation("Perth", PlatformSeek))
	assert.Equal(t, "Perth, Western Australia, Australia", FormatSearchLocation("Perth", PlatformLinkedIn))
}

func TestFormatSearchLocation_Nationwide(t *testing.T) {
	assert.Equal(t, "", FormatSearchLocation("", PlatformSeek))
	assert.Equal(t, "", FormatSearchLocation("Australia", PlatformLinkedIn))
	assert.Equal(t, "", FormatSearchLocation("All Australia", PlatformSeek))
}

func TestSeekLocationPath(t *testing.T) {
	assert.Equal(t, "/jobs", SeekLocationPath(""))
	assert.Equal(t, "/jobs/in-Adelaide-SA", SeekLocationPath("All Adelaide SA"))
	assert.Equal(t, "/jobs/in-Melbourne-VIC", SeekLocationPath("Melbourne, Victoria, Australia"))
}

func TestSeekSearchLocation(t *testing.T) {
	where, ok := SeekSearchLocation("All Adelaide SA")
	assert.True(t, ok)
	assert.Equal(t, "All Adelaide SA", where)

	where, ok = SeekSearchLocation("Adelaide, South Australia, Australia")
	assert.True(t, ok)
	assert.Equal(t, "All Adelaide SA", where)

	_, ok = SeekSearchLocation("Obscureville XYZ")
	assert.False(t, ok)
}

func TestFormatSearchLocation_PreservesCanonical(t *testing.T) {
	assert.Equal(t, "All Melbourne VIC", FormatSearchLocation("All Melbourne VIC", PlatformSeek))
	assert.Equal(t, "Melbourne, Victoria, Australia", FormatSearchLocation("Melbourne, Victoria, Australia", PlatformLinkedIn))
}
