package log

// 本文件是 Go 的 `export_test.go` 惯例：处于 `package log` 内部，因此看得见包级私有状态，
// 又只在测试时编译，**不会进入生产二进制**。它的唯一用途是把「重置一次性开关」这件事
// 暴露给同目录下的**外部**测试包 `log_test`（zz_consumer_check_test.go）。
//
// # 为什么必须有这个文件
//
// `initPasswordOnce` 是包级 `sync.Once`（password.go），在整个测试二进制的生命周期内**只生效一次**。
// 包内测试（password_test.go）靠私有助手 `resetInitPasswordOnce` 逐条复位，
// 但外部测试包拿不到私有标识符，于是曾出现这样的现象：
//
//	go test -run TestAppStartupThenInitAdminFlow   → PASS（单独跑，Once 未被消耗）
//	go test ./internal/base/log/...                → FAIL（全量跑，Once 已被包内用例用掉，
//	                                                  PrintInitPassword 直接走「重复调用」分支，
//	                                                  专用日志文件根本不会被创建）
//
// 测试文件按文件名排序编译执行，`zz_` 前缀又刻意让消费者用例排在最后，
// 因此全量运行时它**必然**落在已被消耗的状态上——这是用例间的状态泄漏，
// 而非生产缺陷：生产进程里 A-1 一辈子只发生一次，Once 的语义正是要的。
//
// 修法上有两条路，取前者：
//   - **暴露复位钩子**（本文件）：消费者用例开跑前先把开关归零，
//     业务流程本身仍只用导出 API（`MustInit` / `PrintInitPassword` / 三个导出常量），
//     黑盒视角完好，且与生产时序一致——「进程内第一次且唯一一次调用」。
//   - 让消费者用例容忍「已打印过」：等于把 A-3 唯一交付路径的断裂变成可接受状态，
//     那条用例也就不再能证明 §十 流程 1 走得通。否决。
func ResetInitPasswordOnceForTest() {
	resetInitPasswordOnce()
}
