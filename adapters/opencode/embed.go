// Package opencode 提供 opencode 适配器的分发资产（内嵌，单一来源）。
//
// 分发判决（2026-09-16）：适配器文件是"拷进别人机器"的分发物，散在代码仓里的
// 副本会随版本漂移（同「协议外置分发 = 漂移死法」）。故资产 go:embed 进 CLI，
// 由 `ousheng adapter install` 幂等写出——升级二进制后每仓重跑一次即同步，
// 且不再依赖源码 clone 路径（go install 场景下源码树常常不在）。
//
// 两个历史事故就地成锁（见 adapter_test.go）：
//  1. opencode.json 的 instructions 只接受路径/glob 字符串数组，写成对象数组
//     {path, description} 会让 opencode 启动即失败；
//  2. opencode 只从 .opencode/plugins/ 加载本地插件，放到 .opencode/plugin.ts
//     不报错但静默失效。
package opencode

import _ "embed"

// PluginTS 是 opencode 插件（三时机协议），写出为
// .opencode/plugins/ousheng-sampler.ts。
//
//go:embed plugin.ts
var PluginTS []byte

// ConfigJSON 是 opencode 配置（MCP 工具），写出为 .opencode/opencode.json。
//
//go:embed opencode.json
var ConfigJSON []byte

// PackageJSON 提供插件依赖 @opencode-ai/plugin，写出为 .opencode/package.json。
//
//go:embed package.json
var PackageJSON []byte

// AgentsMD 是 AGENTS.md 的受管协议段（机器中立：不含路径/身份/环境变量，
// 机器参数只活在 .opencode/ousheng.json）。adapter install 以
// <!-- ousheng:begin --> / <!-- ousheng:end --> 标记幂等写入/替换。
//
//go:embed agents.md
var AgentsMD []byte
