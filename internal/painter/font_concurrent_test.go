package painter

import (
	"sync"
	"testing"

	"fyne.io/fyne/v2"
)

// Text is measured from background goroutines (widgets built off the UI
// thread) while the painter shapes on the UI thread; the shared harfbuzz
// shaper must be serialised for every Shape call.
func TestMeasureTextConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				measureText("Playlist ‹ fi ffl "+string(rune('a'+g)), 11+float32(i%5), fyne.TextStyle{Bold: i%2 == 0}, nil)
			}
		}(g)
	}
	wg.Wait()
}
