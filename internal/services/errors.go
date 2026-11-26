package services

import (
	"fmt"
	"strings"
)

type esError struct {
	msg  string
	orig error
}

func (e *esError) Error() string { return fmt.Sprintf("%s (%s)", e.msg, e.orig.Error()) }
func (e *esError) Unwrap() error { return e.orig }

var errRules = []struct {
	any []string
	msg string
}{
	{[]string{"login failed for user", "password authentication failed", "authentication failed", "no pg_hba.conf entry"},
		"No se pudo autenticar: usuario o contraseña incorrectos"},
	{[]string{"currently in use", "is being accessed by other", "cannot drop the database because it is being"},
		"No se puede eliminar la base de datos porque está en uso"},
	{[]string{"owns a database", "owns database", "owns the database", "is the owner of database"},
		"No se puede eliminar el usuario porque es propietario de una base de datos"},
	{[]string{"is not a valid backup set", "is not a microsoft tape format", "media family", "incorrectly formed", "did not find magic string",
		"does not appear to be a valid archive", "unsupported version", "restore headeronly is terminating", "restore filelistonly is terminating",
		"the backup set holds a backup of a database other than", "unexpected end of file"},
		"El archivo de backup no es válido o está dañado"},
	{[]string{"port is already allocated", "ports are not available", "address already in use", "only one usage of each socket"},
		"El puerto ya está en uso por otro programa o contenedor"},
	{[]string{"pull access denied", "manifest unknown", "not found: manifest", "repository does not exist", "requested access to the resource is denied"},
		"No se encontró la imagen o no hay permiso para descargarla"},
	{[]string{"cannot connect to the docker daemon", "error during connect", "is the docker daemon running", "dockerdesktoplinuxengine",
		"docker_engine", "docker.sock"},
		"Docker no está en ejecución o no es accesible"},
}

// WrapErr traduce errores comunes de SQL Server/PostgreSQL/Docker a un mensaje en español,
// añadiendo el original entre paréntesis. Los errores sin regla conocida se devuelven sin cambios.
func WrapErr(err error) error {
	if err == nil {
		return nil
	}
	var es *esError
	if asEs(err, &es) {
		return err
	}
	low := strings.ToLower(err.Error())
	for _, r := range errRules {
		for _, p := range r.any {
			if strings.Contains(low, p) {
				return &esError{msg: r.msg, orig: err}
			}
		}
	}
	return err
}

func asEs(err error, target **esError) bool {
	for err != nil {
		if e, ok := err.(*esError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// IsPortError indica si el error se debe a que el puerto no se pudo publicar.
func IsPortError(err error) bool {
	if err == nil {
		return false
	}
	low := strings.ToLower(err.Error())
	for _, p := range []string{"port is already allocated", "ports are not available", "address already in use", "only one usage of each socket"} {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
}
