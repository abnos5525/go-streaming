package ptz

import (
	"log"
	"math"
	"sync"
)

const rpcEps = 0.1

var lastZooming sync.Map

func setZooming(dev string, on bool) {
	if on {
		lastZooming.Store(dev, struct{}{})
		return
	}
	lastZooming.Delete(dev)
}

func swapZoomingOff(dev string) bool {
	_, ok := lastZooming.LoadAndDelete(dev)
	return ok
}

func callRPCs(streamID string, in moveMsg) {
	dev := in.DeviceID
	if dev == "" {
		dev = streamID
	}
	switch {
	case math.Abs(in.Zoom) >= rpcEps && math.Abs(in.X) < rpcEps && math.Abs(in.Y) < rpcEps:
		setZooming(dev, true)
		stubCameraZoom(dev, float32(in.Zoom))
	case math.Abs(in.X) < rpcEps && math.Abs(in.Y) < rpcEps && math.Abs(in.Zoom) < rpcEps:
		if swapZoomingOff(dev) {
			stubCameraStopZoom(dev)
		} else {
			stubStop(dev, true, false)
		}
	default:
		setZooming(dev, false)
		stubContinuousMove(dev, float32(in.X), float32(in.Y))
	}
}

func stubContinuousMove(deviceID string, pan, tilt float32) {
	log.Printf("[ptz] ContinuousMove  device=%s  pan=%.2f  tilt=%.2f", deviceID, pan, tilt)
}

func stubStop(deviceID string, panTilt, zoom bool) {
	log.Printf("[ptz] Stop  device=%s  panTilt=%v  zoom=%v", deviceID, panTilt, zoom)
}

func stubCameraZoom(deviceID string, zoom float32) {
	log.Printf("[ptz] CameraZoom  device=%s  zoom=%.2f", deviceID, zoom)
}

func stubCameraStopZoom(deviceID string) {
	log.Printf("[ptz] CameraZoomStop  device=%s", deviceID)
}

func stubAbsoluteMove(deviceID string, pan, tilt, zoom float64) {
	log.Printf("[ptz] AbsoluteMove  device=%s  pan=%.2f  tilt=%.2f  zoom=%.2f", deviceID, pan, tilt, zoom)
}

func stubGetStatus(deviceID string, moving bool) {
	st := "IDLE"
	if moving {
		st = "MOVING"
	}
	log.Printf("[ptz] GetStatus  device=%s  status=%s", deviceID, st)
}