package memory

import (
	"bytes"
	"fmt"
	"slices"
)

type MemoryMarketplace struct {
	products map[int64]*Product
	lastId   int64
}

func NewMemoryMarketplace() *MemoryMarketplace {
	return &MemoryMarketplace{
		products: make(map[int64]*Product),
		lastId:   1,
	}
}

type Product struct {
	title string
	price float64
	stock int
}

func (mm *MemoryMarketplace) Add(title string, price float64, stock int) (int64, error) {
	var err error
	mm.products[mm.lastId], err = NewProduct(title, price, stock)
	if err != nil {
		return 0, err
	}
	defer func() {
		mm.lastId++
	}()
	return mm.lastId, nil
}

func (mm *MemoryMarketplace) List() []byte {
	// getting slice of sorted keys
	keys := make([]int64, 0, len(mm.products))
	for k := range mm.products {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	buf := bytes.NewBuffer(make([]byte, 0, mm.lastId*16))
	for _, key := range keys {
		fmt.Fprintf(buf, "%d. %s — %.2f (stock: %d)\n", key, mm.products[key].title, mm.products[key].price, mm.products[key].stock)
	}
	return buf.Bytes()
}

func (mm *MemoryMarketplace) Buy(id int64, qty int) (int, error) {
	if _, exists := mm.products[id]; !exists {
		return 0, fmt.Errorf("Error: listing not found")
	}
	if qty <= 0 {
		return 0, fmt.Errorf("Error: quantity must be positive")
	} else if qty > mm.products[id].stock {
		return 0, fmt.Errorf("Error: insufficient stock")
	}

	mm.products[id].stock -= qty
	return mm.products[id].stock, nil

}

func (mm *MemoryMarketplace) Delete(id int64) error {
	if _, exists := mm.products[id]; !exists {
		return fmt.Errorf("Error: listing not found")
	}
	mm.products[id] = nil
	delete(mm.products, id)
	return nil
}

func NewProduct(title string, price float64, stock int) (*Product, error) {
	if title == "" {
		return nil, fmt.Errorf("Error: title cannot be empty")
	} else if len(title) > 200 {
		return nil, fmt.Errorf("Error: title is too long")
	}
	if price <= 0 {
		return nil, fmt.Errorf("Error: price must be positive")
	}
	if stock <= 0 {
		return nil, fmt.Errorf("Error: stock must be non-negative")
	}
	return &Product{
		title: title,
		price: price,
		stock: stock,
	}, nil

}
