package store

import (
	"arca/internal/store/models"
)

func (s *Store) ListCategories() ([]models.Category, error) {
	categories := []models.Category{}
	err := s.db.Model(&models.Category{}).Order("position, name").Find(&categories).Error
	return categories, err
}

func (s *Store) CategoryByID(id string) (*models.Category, error) {
	var category models.Category
	if err := s.db.First(&category, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &category, nil
}

func (s *Store) CategoryByName(name string) (*models.Category, error) {
	var category models.Category
	if err := s.db.First(&category, "name = ?", name).Error; err != nil {
		return nil, err
	}
	return &category, nil
}

// CreateCategory appends to the end of the order so a new category never
// displaces the arrangement an administrator already settled on.
func (s *Store) CreateCategory(category *models.Category) error {
	if category.ID == "" {
		category.ID = NewID()
	}
	category.CreatedAt = NowMillis()

	return s.Transaction(func(tx *Store) error {
		var highest *int64
		if err := tx.query().Model(&models.Category{}).
			Select("MAX(position)").Scan(&highest).Error; err != nil {
			return err
		}
		if highest != nil {
			category.Position = *highest + 1
		}
		return tx.query().Create(category).Error
	})
}

func (s *Store) UpdateCategory(id string, changes map[string]any) error {
	return s.db.Model(&models.Category{}).Where("id = ?", id).Updates(changes).Error
}

// DeleteCategory releases its repositories rather than removing them, so losing
// a label can never lose a repository.
func (s *Store) DeleteCategory(id string) error {
	return s.Transaction(func(tx *Store) error {
		if err := tx.query().Model(&models.Repository{}).
			Where("category_id = ?", id).
			Update("category_id", nil).Error; err != nil {
			return err
		}
		return tx.query().Where("id = ?", id).Delete(&models.Category{}).Error
	})
}

func (s *Store) ReorderCategories(ids []string) error {
	return s.Transaction(func(tx *Store) error {
		for position, id := range ids {
			if err := tx.query().Model(&models.Category{}).
				Where("id = ?", id).
				Update("position", position).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
