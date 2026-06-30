package model

// 本文件已迁移：BaseModel 已提取到共享内核 pkg/shared/domain。
//
// 过渡期兼容：这里保留 model.BaseModel 作为共享内核的类型别名，
// 让尚未搬迁的旧 model（Circle/Post/Comment 等）继续可用。
// 类型别名（带 =）会保留方法集，因此 BeforeCreate 钩子仍然生效。
//
// 新代码（领域实体）请直接内嵌 sharedomain.BaseModel，
// 不要再使用 model.BaseModel——本别名会在所有领域搬迁完成后随
// pkg/server/model 包一起删除。
//
// noqa:deprecated —— 详见 docs/refactor-1-migration-progress.md
import sharedomain "interestBar/pkg/shared/domain"

// BaseModel 是 sharedomain.BaseModel 的过渡期别名（类型别名，保留方法集）。
type BaseModel = sharedomain.BaseModel
