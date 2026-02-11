package migrate

import (
	"gorm.io/gorm"
)

func init() {
	// [fork] 初始化现有分组的 sort_order 为 id 值
	RegisterAfterAutoMigration(Migration{
		Version: 3,
		Up:      initGroupSortOrder,
	})
}

// 003: set sort_order = id for existing groups where sort_order is 0
func initGroupSortOrder(db *gorm.DB) error {
	return db.Table("groups").
		Where("sort_order = 0 OR sort_order IS NULL").
		Update("sort_order", gorm.Expr("id")).Error
}
