package scene

import (
	"ray-tracer/color"
	"ray-tracer/config"
	"ray-tracer/core"
	"ray-tracer/material"
	"ray-tracer/object"
	"ray-tracer/texture"
	"ray-tracer/vector"
)

func Earth() {
	world := core.EmptyHittableList()

	earthTexture := texture.NewImage("assets/earthmap.jpg")
	earthSurface := material.NewTexturedLambertian(earthTexture)
	globe := object.NewSphere(vector.NewPoint3(0, 0, 0), 2, earthSurface)

	world.Add(globe)

	writer := config.NewWriter()

	// Camera
	camera := core.NewCamera()
	camera.AspectRatio = 16.0 / 9
	camera.ImageWidth = 400
	camera.SamplesPerPixel = 100
	camera.MaxDepth = 50
	camera.Background = color.NewColor(0.7, 0.8, 1.0)

	camera.VerticalFieldOfView = 20
	camera.LookFrom = vector.NewPoint3(0, 0, 12)
	camera.LookAt = vector.NewPoint3(0, 0, 0)
	camera.VectorUp = vector.NewVector3(0, 1, 0)

	camera.DefocusAngle = 0
	camera.FocusDistance = 10

	if err := camera.Render(world, writer); err != nil {
		panic(err)
	}
}
