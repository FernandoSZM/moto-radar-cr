package main

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type RegistroPrecio struct {
	PrecioCRC int       `json:"precio_crc"`
	Fecha     time.Time `json:"fecha"`
}

type Moto struct {
	ID          int    `json:"id"`
	Marca       string `json:"marca"`
	Modelo      string `json:"modelo"`
	Anio        int    `json:"anio"`
	Kilometraje int    `json:"kilometraje"`
	PrecioCRC   int    `json:"precio_crc"`
	AnuncioURL  string `json:"anuncio_url,omitempty"`
}

const columnasMoto = "id, marca, modelo, anio, kilometraje, precio_crc, anuncio_url"

func leerMoto(fila interface{ Scan(...any) error }) (Moto, error) {
	var moto Moto
	err := fila.Scan(&moto.ID, &moto.Marca, &moto.Modelo, &moto.Anio,
		&moto.Kilometraje, &moto.PrecioCRC, &moto.AnuncioURL)
	return moto, err
}

func listarMotos(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	consulta := "SELECT " + columnasMoto + " FROM motos"
	var argumentos []any
	if r.URL.Query().Has("precio_max") {
		maximo, err := strconv.Atoi(r.URL.Query().Get("precio_max"))
		if err != nil || maximo < 0 {
			responderJSON(w, http.StatusBadRequest, map[string]string{
				"error": "El presupuesto máximo debe ser un entero de cero o más colones.",
			})
			return
		}
		consulta += " WHERE precio_crc <= ?"
		argumentos = append(argumentos, maximo)
	}
	filas, err := db.QueryContext(r.Context(), consulta+" ORDER BY id", argumentos...)
	if err != nil {
		responderErrorBD(w, err)
		return
	}
	defer filas.Close()

	lista := []Moto{}
	for filas.Next() {
		moto, err := leerMoto(filas)
		if err != nil {
			responderErrorBD(w, err)
			return
		}
		lista = append(lista, moto)
	}
	if err := filas.Err(); err != nil {
		responderErrorBD(w, err)
		return
	}

	responderJSON(w, http.StatusOK, lista)
}

func responderErrorBD(w http.ResponseWriter, err error) {
	log.Printf("Error de SQLite: %v", err)
	responderJSON(w, http.StatusInternalServerError, map[string]string{
		"error": "Ocurrió un error al acceder a la base de datos.",
	})
}

func validarMoto(moto Moto) string {
	if moto.Marca == "" || moto.Modelo == "" || moto.Anio <= 0 ||
		moto.Kilometraje < 0 || moto.PrecioCRC <= 0 {
		return "Indicá marca, modelo, año y precio positivo. El kilometraje debe ser cero o mayor."
	}
	if moto.AnuncioURL != "" {
		enlace, err := url.Parse(moto.AnuncioURL)
		if err != nil || enlace.Hostname() == "" || (enlace.Scheme != "http" && enlace.Scheme != "https") {
			return "Ingresá un enlace al anuncio que empiece con http:// o https:// y tenga un dominio."
		}
	}
	return ""
}

func crearMoto(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var moto Moto
	if err := json.NewDecoder(r.Body).Decode(&moto); err != nil {
		responderJSON(w, http.StatusBadRequest, map[string]string{
			"error": "El cuerpo debe contener un objeto JSON válido.",
		})
		return
	}

	moto.Marca = strings.TrimSpace(moto.Marca)
	moto.Modelo = strings.TrimSpace(moto.Modelo)
	moto.AnuncioURL = strings.TrimSpace(moto.AnuncioURL)
	if mensaje := validarMoto(moto); mensaje != "" {
		responderJSON(w, http.StatusBadRequest, map[string]string{"error": mensaje})
		return
	}

	ctx := r.Context()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		responderErrorBD(w, err)
		return
	}
	defer tx.Rollback()

	resultado, err := tx.ExecContext(ctx, `
		INSERT INTO motos (marca, modelo, anio, kilometraje, precio_crc, anuncio_url)
		VALUES (?, ?, ?, ?, ?, ?)
	`, moto.Marca, moto.Modelo, moto.Anio, moto.Kilometraje, moto.PrecioCRC, moto.AnuncioURL)
	if err != nil {
		responderErrorBD(w, err)
		return
	}

	id, err := resultado.LastInsertId()
	if err != nil {
		responderErrorBD(w, err)
		return
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO precios (moto_id, precio_crc, fecha)
		VALUES (?, ?, ?)
	`, id, moto.PrecioCRC, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		responderErrorBD(w, err)
		return
	}

	if err := tx.Commit(); err != nil {
		responderErrorBD(w, err)
		return
	}

	moto.ID = int(id)
	responderJSON(w, http.StatusCreated, moto)
}

func responderJSON(w http.ResponseWriter, estado int, datos any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(estado)
	if err := json.NewEncoder(w).Encode(datos); err != nil {
		log.Printf("Error al enviar JSON: %v", err)
	}
}

func obtenerMoto(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		responderJSON(w, http.StatusBadRequest, map[string]string{
			"error": "El ID debe ser un entero positivo.",
		})
		return
	}

	moto, err := leerMoto(db.QueryRowContext(r.Context(),
		"SELECT "+columnasMoto+" FROM motos WHERE id = ?", id))
	if err == sql.ErrNoRows {
		responderJSON(w, http.StatusNotFound, map[string]string{
			"error": "No se encontró una moto con ese ID.",
		})
		return
	}
	if err != nil {
		responderErrorBD(w, err)
		return
	}

	responderJSON(w, http.StatusOK, moto)
}

func actualizarMoto(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		responderJSON(w, http.StatusBadRequest, map[string]string{
			"error": "El ID debe ser un entero positivo.",
		})
		return
	}

	var cambio struct {
		Marca       *string `json:"marca"`
		Modelo      *string `json:"modelo"`
		Anio        *int    `json:"anio"`
		Kilometraje *int    `json:"kilometraje"`
		PrecioCRC   *int    `json:"precio_crc"`
		AnuncioURL  *string `json:"anuncio_url"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cambio); err != nil || decoder.Decode(new(any)) != io.EOF {
		responderJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Enviá un objeto JSON con campos editables de la moto.",
		})
		return
	}
	if cambio.Marca == nil && cambio.Modelo == nil && cambio.Anio == nil &&
		cambio.Kilometraje == nil && cambio.PrecioCRC == nil && cambio.AnuncioURL == nil {
		responderJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Enviá al menos un campo para editar.",
		})
		return
	}

	ctx := r.Context()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		responderErrorBD(w, err)
		return
	}
	defer tx.Rollback()

	moto, err := leerMoto(tx.QueryRowContext(ctx,
		"SELECT "+columnasMoto+" FROM motos WHERE id = ?", id))
	if err == sql.ErrNoRows {
		responderJSON(w, http.StatusNotFound, map[string]string{
			"error": "No se encontró una moto con ese ID.",
		})
		return
	}
	if err != nil {
		responderErrorBD(w, err)
		return
	}

	anterior := moto
	if cambio.Marca != nil {
		moto.Marca = strings.TrimSpace(*cambio.Marca)
	}
	if cambio.Modelo != nil {
		moto.Modelo = strings.TrimSpace(*cambio.Modelo)
	}
	if cambio.Anio != nil {
		moto.Anio = *cambio.Anio
	}
	if cambio.Kilometraje != nil {
		moto.Kilometraje = *cambio.Kilometraje
	}
	if cambio.PrecioCRC != nil {
		moto.PrecioCRC = *cambio.PrecioCRC
	}
	if cambio.AnuncioURL != nil {
		moto.AnuncioURL = strings.TrimSpace(*cambio.AnuncioURL)
	}
	if mensaje := validarMoto(moto); mensaje != "" {
		responderJSON(w, http.StatusBadRequest, map[string]string{"error": mensaje})
		return
	}

	if moto != anterior {
		_, err := tx.ExecContext(ctx, `
			UPDATE motos
			SET marca = ?, modelo = ?, anio = ?, kilometraje = ?, precio_crc = ?, anuncio_url = ?
			WHERE id = ?
		`, moto.Marca, moto.Modelo, moto.Anio, moto.Kilometraje, moto.PrecioCRC, moto.AnuncioURL, id)
		if err != nil {
			responderErrorBD(w, err)
			return
		}
	}

	if moto.PrecioCRC != anterior.PrecioCRC {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO precios (moto_id, precio_crc, fecha)
			VALUES (?, ?, ?)
		`, id, moto.PrecioCRC, time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			responderErrorBD(w, err)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		responderErrorBD(w, err)
		return
	}

	responderJSON(w, http.StatusOK, moto)
}

func eliminarMoto(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		responderJSON(w, http.StatusBadRequest, map[string]string{
			"error": "El ID debe ser un entero positivo.",
		})
		return
	}

	resultado, err := db.ExecContext(r.Context(), "DELETE FROM motos WHERE id = ?", id)
	if err != nil {
		responderErrorBD(w, err)
		return
	}

	filasAfectadas, err := resultado.RowsAffected()
	if err != nil {
		responderErrorBD(w, err)
		return
	}
	if filasAfectadas == 0 {
		responderJSON(w, http.StatusNotFound, map[string]string{
			"error": "No se encontró una moto con ese ID.",
		})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func listarPrecios(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		responderJSON(w, http.StatusBadRequest, map[string]string{
			"error": "El ID debe ser un entero positivo.",
		})
		return
	}

	filas, err := db.QueryContext(r.Context(), `
		SELECT precio_crc, fecha
		FROM precios
		WHERE moto_id = ?
		ORDER BY id
	`, id)
	if err != nil {
		responderErrorBD(w, err)
		return
	}
	defer filas.Close()

	historial := []RegistroPrecio{}
	for filas.Next() {
		var registro RegistroPrecio
		var fecha string
		if err := filas.Scan(&registro.PrecioCRC, &fecha); err != nil {
			responderErrorBD(w, err)
			return
		}

		registro.Fecha, err = time.Parse(time.RFC3339Nano, fecha)
		if err != nil {
			responderErrorBD(w, err)
			return
		}
		historial = append(historial, registro)
	}

	if err := filas.Err(); err != nil {
		responderErrorBD(w, err)
		return
	}

	if len(historial) == 0 {
		responderJSON(w, http.StatusNotFound, map[string]string{
			"error": "No se encontró una moto con ese ID.",
		})
		return
	}

	responderJSON(w, http.StatusOK, historial)
}

func main() {
	db, err := abrirBaseDeDatos("moto-radar.db")
	if err != nil {
		log.Fatalf("Error al preparar SQLite: %v", err)
	}
	defer db.Close()

	log.Println("SQLite listo en moto-radar.db")

	log.Println("API en http://localhost:8080/motos")
	log.Fatal(http.ListenAndServe("localhost:8080", rutas(db)))
}

func rutas(db *sql.DB) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /motos", func(w http.ResponseWriter, r *http.Request) {
		listarMotos(w, r, db)
	})
	mux.HandleFunc("POST /motos", func(w http.ResponseWriter, r *http.Request) {
		crearMoto(w, r, db)
	})

	mux.HandleFunc("GET /motos/{id}", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("obtenerMoto SQLite: id=%q", r.PathValue("id"))
		obtenerMoto(w, r, db)
	})

	mux.HandleFunc("GET /motos/{id}/precios", func(w http.ResponseWriter, r *http.Request) {
		listarPrecios(w, r, db)
	})

	mux.HandleFunc("PATCH /motos/{id}", func(w http.ResponseWriter, r *http.Request) {
		actualizarMoto(w, r, db)
	})

	mux.HandleFunc("DELETE /motos/{id}", func(w http.ResponseWriter, r *http.Request) {
		eliminarMoto(w, r, db)
	})

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})

	return mux
}
