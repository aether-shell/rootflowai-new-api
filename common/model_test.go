package common

import "testing"

func TestIsImageGenerationModelGPTImage2Family(t *testing.T) {
	for _, model := range []string{
		"gpt-image-2",
		"gpt-image-2.5-flare",
		"gpt-image-2.5-sunburst",
	} {
		if !IsImageGenerationModel(model) {
			t.Errorf("%s was not recognized as an image generation model", model)
		}
	}

	if IsImageGenerationModel("gpt-5.2") {
		t.Fatal("text model was recognized as an image generation model")
	}
}
