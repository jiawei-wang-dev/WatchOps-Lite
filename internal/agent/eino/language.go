package eino

import "unicode"

func prefersChinese(message string) bool {
	for _, character := range message {
		if unicode.Is(unicode.Han, character) {
			return true
		}
	}
	return false
}

func localizedText(chinese bool, english, chineseText string) string {
	if chinese {
		return chineseText
	}
	return english
}

func responseLanguage(message string) string {
	if prefersChinese(message) {
		return "zh"
	}
	return "en"
}
