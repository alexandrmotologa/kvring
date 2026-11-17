package ring

// AdmissionController predicts KV-cache memory expansion prior to dispatching requests.
type AdmissionController struct {
	totalGpuMemoryBytes int64   // Default 80 GB (80 * 1024^3)
	bytesPerToken       int64   // Bytes per token per GPU (e.g. 80KB for 70B TP4)
	vramThreshold       float64 // Maximum safe threshold (default 0.85)
}

// NewAdmissionController constructs a pre-flight admission evaluator.
func NewAdmissionController(totalGpuVramGB int, bytesPerToken int64, threshold float64) *AdmissionController {
	if totalGpuVramGB <= 0 {
		totalGpuVramGB = 80 // Default H100 80GB
	}
	if bytesPerToken <= 0 {
		bytesPerToken = 80 * 1024 // 80 KB per token per GPU for 70B TP4
	}
	if threshold <= 0 || threshold > 1.0 {
		threshold = 0.85
	}
	return &AdmissionController{
		totalGpuMemoryBytes: int64(totalGpuVramGB) * 1024 * 1024 * 1024,
		bytesPerToken:       bytesPerToken,
		vramThreshold:       threshold,
	}
}

// EstimateDeltaVramRatio computes the expected VRAM percentage increase for a given token count.
func (ac *AdmissionController) EstimateDeltaVramRatio(estimatedTokens int) float64 {
	if estimatedTokens <= 0 {
		return 0.0
	}
	deltaBytes := int64(estimatedTokens) * ac.bytesPerToken
	return float64(deltaBytes) / float64(ac.totalGpuMemoryBytes)
}

// CanAccept verifies if a worker can absorb the request without crossing the VRAM limit.
func (ac *AdmissionController) CanAccept(w *WorkerNode, estimatedTokens int) bool {
	if !w.Healthy.Load() {
		return false
	}
	currentRatio := w.GetVramRatio()
	delta := ac.EstimateDeltaVramRatio(estimatedTokens)
	return (currentRatio + delta) < ac.vramThreshold
}
