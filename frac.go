// Copyright 2020 Andrew Quinn. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gofrac

import (
	"errors"
	"math"
	"math/cmplx"
	"runtime"
	"sync"
)

//var debug = log.New(os.Stdout, "DEBUG: ", log.LstdFlags)

// Fraccer maps a point in the complex plane to the result of a fractal calculation
type Fraccer interface {
	// Frac performs iterations of a fractal equation for a complex number
	// given by loc.
	Frac(loc complex128) *Result
	SetMaxIterations(iterations int) error
	FracDataGetter
}

type FracDataGetter interface {
	Data() *FracData
}

// FracIt applies the fractal calculation given by f to every sample in the
// domain d. The maximum number of iterations to be performed is given by
// iterations.
func FracIt(d DomainReader, f Fraccer, iterations int) (*Results, error) {
	err := f.SetMaxIterations(iterations)
	if err != nil {
		return nil, err
	}

	rows, cols := d.Dimensions()
	if cols < 1 || rows < 1 {
		return nil, errors.New("gofrac: the domain must be sampled at least once along each axis")
	}

	results := NewResults(rows, cols, iterations)
	defer results.Done()

	rowJobs := make(chan int, rows)

	numWorkers := runtime.NumCPU()
	wg := sync.WaitGroup{}
	for worker := 0; worker < numWorkers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for row := range rowJobs {
				for col := 0; col < cols; col++ {
					loc, err := d.At(col, row)
					if err != nil {
						panic(err)
					}
					r := f.Frac(loc)
					results.SetResult(row, col, r.Z, r.C, r.Iterations)
				}
			}
		}()
	}

	for row := 0; row < rows; row++ {
		rowJobs <- row
	}

	close(rowJobs)
	wg.Wait()

	return &results, nil
}

type FracData struct {
	// Radius is the bailout radius of a fractal calculation.
	Radius float64

	// MaxIterations is the number of iterations at which an iterated input value is considered convergent if it hasn't
	// exceeded Radius.
	MaxIterations int

	// degree is the degree of the complex polynomial function to be iterated.
	degree float64

	// the inverse of the log of degree
	logDegreeInv float64
}

func (f *FracData) Data() *FracData {
	return f
}

func (f *FracData) SetRadius(r float64) {
	f.Radius = r
}

func (f *FracData) SetMaxIterations(n int) error {
	if n < 1 {
		return errors.New("gofrac: the maximum iteration count must be greater than zero")
	}
	f.MaxIterations = n
	return nil
}

func (f *FracData) SetDegree(d float64) {
	f.degree = d
	f.logDegreeInv = 1 / math.Log(d)
}

// Quadratic stores the information needed by a quadratic fractal.
type Quadratic struct {
	FracData
}

func getMod2(z complex128) float64 {
	a, b := real(z), imag(z)
	return a*a + b+b
}

func (q Quadratic) q(z complex128, c complex128) *Result {
	count := 0
	r2 := q.Radius * q.Radius
	maxIt := q.MaxIterations-1
	for mod2 := getMod2(z); mod2 <= r2; mod2 = getMod2(z) {
		z = z*z + c
		if count == maxIt {
			break
		}
		mod2 = getMod2(z)
		count++
	}
	return &Result{
		Z:          z,
		C:          c,
		Iterations: count,
	}
}

// The Mandelbrot set, which results from iterating the function
// f_c(z) = z^2 + c, for all complex numbers c and z_0 = 0.
type Mandelbrot struct {
	Quadratic
}

func NewQuadratic(radius float64) Quadratic {
	q := Quadratic{
		FracData{
			Radius: radius,
		},
	}
	q.SetDegree(2.0)
	return q
}

// NewMandelbrot constructs a Mandelbrot struct with a given bailout radius.
func NewMandelbrot(radius float64) *Mandelbrot {
	return &Mandelbrot{
		Quadratic: NewQuadratic(radius),
	}
}

// isCardioidOrP2Bulb detects points within the first and second order
// convergent zones of the Mandelbrot set.
func isCardioidOrP2Bulb(z complex128, maxIt int) bool {
	reZ := real(z)
	imZ := imag(z)

	rzp := reZ - 0.25
	imz2 := imZ * imZ

	// Cardioid test:
	// q = \left(\Re(z) - \frac{1}{4}\right)^2 + \Im(z)^2
	// q \left( q \left( \Re(z) - \frac{1}{4} \right ) \right ) \leq \frac{\Im(z)^2}{4}
	q := rzp*rzp + imz2
	isCardiod := q*(q+rzp) <= 0.25*imz2

	if isCardiod {
		return true
	}

	// P2 test:
	// \left(\Re\left(z \right ) + 1 \right )^2 + \Im\left(z\right)^2 \leq \left(\frac{1}{4}\right)^2
	isP2Bulb := (reZ+1)*(reZ+1)+imZ*imZ <= 0.0625
	return isP2Bulb

}

func (m Mandelbrot) Frac(loc complex128) *Result {
	if isCardioidOrP2Bulb(loc, m.MaxIterations) {
		return &Result{
			Z:          loc,
			C:          0,
			Iterations: m.MaxIterations - 1,
		}
	}
	return m.q(0, loc)
}

// JuliaQ is the quadratic Julia set, which results from iterating the function
// f_C(z) = z^2 + C for a all complex numbers z and a given complex number C.
type JuliaQ struct {
	Quadratic
	C complex128
}

// NewJuliaQ constructs a new JuliaQ struct with a given bailout radius and a
// complex parameter c corresponding to the C in f_C(z) = z^2 + C.
func NewJuliaQ(radius float64, c complex128) *JuliaQ {
	return &JuliaQ{
		Quadratic: NewQuadratic(radius),
		C:         c,
	}
}

func (j JuliaQ) Frac(loc complex128) *Result {
	return j.q(loc, j.C)
}

// JuliaR is a Julia set generated by a rational complex function given by
// f_C(z) = P(z) / Q(z) + C.
type JuliaR struct {
	FracData
	P func(complex128) complex128
	Q func(complex128) complex128
	C complex128
}

func (r JuliaR) Frac(loc complex128) *Result {
	z := loc
	count := 0
	for mod := cmplx.Abs(z); mod <= r.Radius; mod, count = cmplx.Abs(z), count+1 {
		z = r.P(z)/r.Q(z) + r.C
		if count == r.MaxIterations-1 {
			break
		}
	}
	return &Result{
		Z:          z,
		C:          r.C,
		Iterations: count,
	}
}

// Polynomiograph contains the data necessary to perform the style of
// computations described by Kalantari (et al) in several publications. See,
// for example, https://www.tandfonline.com/doi/full/10.1080/17513472.2019.1600959
type Polynomiograph struct {
	FracData
	// B is a root-finding rational polynomial of the family used in Newton's,
	// Halley's, or higher-order methods. See the link above for a generalized
	// form.
	B   func(z complex128) complex128

	// F maps the input location to an initial value.
	F   func(loc complex128) complex128

	// G is the transformation applied to the value c_n after z_{n+1} has been
	// calculated during iterations. That is,
	//    z_{n+1} = B(z_n) + c_n,
	//    c_{n+1} = G(c_n)
	G   func(c complex128) complex128

	// eps is the convergence threshold.
	eps float64
}

// MandelPG is a Mandelbrot-style polynomiograph
type MandelPG Polynomiograph

// CCMap is simply a short name for a function f: C -> C
type CCMap func(complex128) complex128

// NewMandePG returns a new structure with the members required for
// Mandelbrot-style polynomiograph calculations.
func NewMandelPG(eps float64, B CCMap, F CCMap, G CCMap) *MandelPG {
	mandel := MandelPG {
		B: B,
		F: F,
		G: G,
		eps : eps,
	}
	return &mandel
}

func (m MandelPG) Frac(loc complex128) *Result {
	z := loc
	c := m.F(loc)
	count := 0
	for {
		zNext := m.B(z) - c
		if cmplx.Abs(zNext - z) < m.eps {
			break
		}
		c = m.G(c)
		z = zNext
		if count == m.MaxIterations-1 {
			break
		}
		count++
	}

	return &Result{
		Z:          z,
		C:          c,
		Iterations: count,
		NFactor:    0,
	}
}
