package repo

import (
	"context"

	"gorm.io/gorm"
)

// BackupRepo 备份数据访问：SQLite 在线一致性快照（VACUUM INTO，
// 读写不互斥、快照原子落盘）。PostgreSQL 模式不支持此路径（由
// service 层在调用前拒绝），Exec 报方言错误仅作兜底。
type BackupRepo interface {
	// VacuumInto 把当前库快照写至 dest（文件必须不存在，SQLite 语义）。
	VacuumInto(ctx context.Context, dest string) error
}

type backupRepo struct{ gdb *gorm.DB }

func NewBackupRepo(gdb *gorm.DB) BackupRepo { return backupRepo{gdb} }

func (r backupRepo) VacuumInto(ctx context.Context, dest string) error {
	return r.gdb.WithContext(ctx).Exec("VACUUM INTO ?", dest).Error
}
