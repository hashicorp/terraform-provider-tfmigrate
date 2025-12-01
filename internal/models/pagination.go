package models

type Pagination struct {
	CurrentPage int         `json:"current-page"`
	PageSize    int         `json:"page-size"`
	PrevPage    interface{} `json:"prev-page"`
	NextPage    interface{} `json:"next-page"`
	TotalPages  int         `json:"total-pages"`
	TotalCount  int         `json:"total-count"`
}
