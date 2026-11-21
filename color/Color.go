package color

type Color struct {
	R, G, B float64
}

func NewColor(r float64, g float64, b float64) *Color {
	return &Color{r, g, b}
}

func (c *Color) CopyFrom(other *Color) {
	c.R = other.R
	c.G = other.G
	c.B = other.B
}

func (c *Color) Add(other *Color) *Color {
	return NewColor(c.R+other.R, c.G+other.G, c.B+other.B)
}

func (c *Color) Multiply(other *Color) *Color {
	return NewColor(c.R*other.R, c.G*other.G, c.B*other.B)
}

func (c *Color) AddInPlace(other *Color) {
	c.R += other.R
	c.G += other.G
	c.B += other.B
}

func (c *Color) MultiplyBy(multiplier float64) *Color {
	return NewColor(c.R*multiplier, c.G*multiplier, c.B*multiplier)
}
