package domain

import "io"

type File struct {
	Content     io.ReadCloser
	Size        int64
	ContentType string
}

func NewFile(content io.ReadCloser, size int64, contentType string) File {
	return File{
		Content:     content,
		Size:        size,
		ContentType: contentType,
	}
}
