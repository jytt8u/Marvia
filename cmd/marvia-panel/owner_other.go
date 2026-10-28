//go:build !unix

package main

// keepOwner на Windows не нужен: там у файлов нет хозяина в том смысле,
// который мешал бы службе открыть базу.
func keepOwner(string) {}
