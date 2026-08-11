package models

// PaginatedResponse wraps a list response with pagination metadata
type PaginatedResponse struct {
	Data       interface{} `json:"data"`
	Total      int         `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"pageSize"`
	TotalPages int         `json:"totalPages"`
}

// PaginationQuery untuk extract pagination params dari query string
type PaginationQuery struct {
	Page     int `form:"page,default=1"`
	PageSize int `form:"pageSize,default=20"`
}

func (p *PaginationQuery) Offset() int {
	if p.Page < 1 {
		p.Page = 1
	}
	return (p.Page - 1) * p.PageSize
}
