package mapstructure

import (
	"strings"
	"testing"
)

// 复用定长缓冲 × 源形状的矩阵对照测试。
// 背景：分批装载报文到同一个定长数组时，已填充目标此前会跳过
// 输入类型检查与长度校验，导致长源/标量源直接 panic。
// 矩阵维度：
//   - 目标：零值目标（全新缓冲） vs 已填充目标（复用旧缓冲）
//   - 源：长源（超长）、短源（不足长）、等长源、标量源
//   - 模式：严格 vs WeaklyTypedInput
//
// 两条目标路径的报错口径必须一致，且不得 panic。

// decodeIntoFilled 先把目标填充为非零值，再用给定源解码进同一缓冲，
// 模拟“分批装载复用行缓冲”的场景。
func decodeIntoFilled(t *testing.T, weak bool, src any, dst *[4]int) error {
	t.Helper()
	*dst = [4]int{9, 9, 9, 9}
	if weak {
		return WeakDecode(src, dst)
	}
	return Decode(src, dst)
}

func TestDecodeArray_ReusedBuffer_LongSource(t *testing.T) {
	longSrc := []any{1, 2, 3, 4, 5}

	// 零值目标：既有长度校验错误，读数逐字固定，不得回退。
	var zeroDst [4]int
	zeroErr := Decode(longSrc, &zeroDst)
	if zeroErr == nil {
		t.Fatal("zero target: expected length error, got nil")
	}
	wantMsg := "expected source data to have length less or equal to 4, got 5"
	if !strings.Contains(zeroErr.Error(), wantMsg) {
		t.Fatalf("zero target: error %q does not contain %q", zeroErr.Error(), wantMsg)
	}

	// 已填充目标（复用旧缓冲）：必须得到同口径的长度校验错误而非崩溃。
	var filledDst [4]int
	filledErr := decodeIntoFilled(t, false, longSrc, &filledDst)
	if filledErr == nil {
		t.Fatal("filled target: expected length error, got nil")
	}
	if !strings.Contains(filledErr.Error(), wantMsg) {
		t.Fatalf("filled target: error %q does not contain %q", filledErr.Error(), wantMsg)
	}

	// 弱类型模式下同样不得崩溃。
	var weakDst [4]int
	weakErr := decodeIntoFilled(t, true, longSrc, &weakDst)
	if weakErr == nil {
		t.Fatal("weak filled target: expected length error, got nil")
	}
	if !strings.Contains(weakErr.Error(), wantMsg) {
		t.Fatalf("weak filled target: error %q does not contain %q", weakErr.Error(), wantMsg)
	}
}

func TestDecodeArray_ReusedBuffer_ShortSource(t *testing.T) {
	// 短批次装入旧缓冲：保留既有语义——覆写前缀，尾部保留旧值。
	var dst [4]int
	if err := decodeIntoFilled(t, false, []any{1, 2}, &dst); err != nil {
		t.Fatalf("short source into filled target: %v", err)
	}
	if dst != [4]int{1, 2, 9, 9} {
		t.Fatalf("short source into filled target: got %v, want [1 2 9 9]", dst)
	}

	// 对照：短源装入零值目标，尾部为零值。
	var zeroDst [4]int
	if err := Decode([]any{1, 2}, &zeroDst); err != nil {
		t.Fatalf("short source into zero target: %v", err)
	}
	if zeroDst != [4]int{1, 2, 0, 0} {
		t.Fatalf("short source into zero target: got %v, want [1 2 0 0]", zeroDst)
	}

	// 等长源装入旧缓冲：整体覆写。
	var fullDst [4]int
	if err := decodeIntoFilled(t, false, []any{1, 2, 3, 4}, &fullDst); err != nil {
		t.Fatalf("exact source into filled target: %v", err)
	}
	if fullDst != [4]int{1, 2, 3, 4} {
		t.Fatalf("exact source into filled target: got %v, want [1 2 3 4]", fullDst)
	}
}

func TestDecodeArray_ReusedBuffer_ScalarSource(t *testing.T) {
	// 严格模式标量源：两条目标路径都必须报类型错误而非崩溃。
	var zeroDst [4]int
	zeroErr := Decode(42, &zeroDst)
	if zeroErr == nil {
		t.Fatal("zero target: expected type error, got nil")
	}
	wantMsg := "source data must be an array or slice, got int"
	if !strings.Contains(zeroErr.Error(), wantMsg) {
		t.Fatalf("zero target: error %q does not contain %q", zeroErr.Error(), wantMsg)
	}

	var filledDst [4]int
	filledErr := decodeIntoFilled(t, false, 42, &filledDst)
	if filledErr == nil {
		t.Fatal("filled target: expected type error, got nil")
	}
	if !strings.Contains(filledErr.Error(), wantMsg) {
		t.Fatalf("filled target: error %q does not contain %q", filledErr.Error(), wantMsg)
	}

	// 弱类型标量源：提升为单元素数组，两条路径都不得崩溃。
	var zeroWeak [4]int
	if err := WeakDecode(7, &zeroWeak); err != nil {
		t.Fatalf("weak scalar into zero target: %v", err)
	}
	if zeroWeak != [4]int{7, 0, 0, 0} {
		t.Fatalf("weak scalar into zero target: got %v, want [7 0 0 0]", zeroWeak)
	}

	var filledWeak [4]int
	if err := decodeIntoFilled(t, true, 7, &filledWeak); err != nil {
		t.Fatalf("weak scalar into filled target: %v", err)
	}
	if filledWeak != [4]int{7, 9, 9, 9} {
		t.Fatalf("weak scalar into filled target: got %v, want [7 9 9 9]", filledWeak)
	}
}

// 分批装载的端到端场景：同一缓冲先后装两批，第二批超长时报错而非崩溃。
func TestDecodeArray_ReusedBuffer_BatchedLoads(t *testing.T) {
	var buf [4]int
	if err := Decode([]any{1, 2}, &buf); err != nil {
		t.Fatalf("batch 1: %v", err)
	}
	if buf != [4]int{1, 2, 0, 0} {
		t.Fatalf("batch 1: got %v, want [1 2 0 0]", buf)
	}

	err := Decode([]any{1, 2, 3, 4, 5, 6}, &buf)
	if err == nil {
		t.Fatal("batch 2: expected length error, got nil")
	}
	wantMsg := "expected source data to have length less or equal to 4, got 6"
	if !strings.Contains(err.Error(), wantMsg) {
		t.Fatalf("batch 2: error %q does not contain %q", err.Error(), wantMsg)
	}
}
