// +build portal

/*
 *    Copyright (c) 2018 Unrud<unrud@outlook.com>
 *
 *    This file is part of Remote-Touchpad.
 *
 *    Foobar is free software: you can redistribute it and/or modify
 *    it under the terms of the GNU General Public License as published by
 *    the Free Software Foundation, either version 3 of the License, or
 *    (at your option) any later version.
 *
 *    Remote-Touchpad is distributed in the hope that it will be useful,
 *    but WITHOUT ANY WARRANTY; without even the implied warranty of
 *    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *    GNU General Public License for more details.
 *
 *   You should have received a copy of the GNU General Public License
 *   along with Foobar.  If not, see <http://www.gnu.org/licenses/>.
 */

package main

import (
	dbus "anonscm.debian.org/git/pkg-go/packages/golang-dbus.git"
	"errors"
	"fmt"
	"sync"
)

const (
	deviceKeyboard uint32 = 1
	devicePointer  uint32 = 2

	btnReleased uint32 = 0
	btnPressed  uint32 = 1

	btnLeft   int32 = 0x110
	btnRight  int32 = 0x111
	btnMiddle int32 = 0x112
)

type portalPlugin struct {
	bus           *dbus.Conn
	remoteDesktop dbus.BusObject
	sessionHandle dbus.ObjectPath
	lock          sync.RWMutex
}

func InitPortalPlugin() (Plugin, error) {
	bus, err := dbus.SessionBusPrivate()
	if err != nil {
		return nil, UnsupportedPlatformError{err}
	}
	cleanupBus := true
	defer func() {
		if cleanupBus {
			bus.Close()
		}
	}()
	err = bus.Auth(nil)
	if err != nil {
		return nil, UnsupportedPlatformError{err}
	}
	err = bus.Hello()
	if err != nil {
		return nil, UnsupportedPlatformError{err}
	}
	remoteDesktop := bus.Object("org.freedesktop.portal.Desktop",
		"/org/freedesktop/portal/desktop")
	availableDeviceTypesV, err := remoteDesktop.GetProperty(
		"org.freedesktop.portal.RemoteDesktop.AvailableDeviceTypes")
	if err != nil {
		return nil, UnsupportedPlatformError{err}
	}
	availableDeviceTypes, ok := availableDeviceTypesV.Value().(uint32)
	if !ok {
		return nil, UnsupportedPlatformError{errors.New(
			"unexpected 'AvailableDeviceTypes' return type")}
	}
	if availableDeviceTypes&deviceKeyboard == 0 ||
		availableDeviceTypes&devicePointer == 0 {
		return nil, UnsupportedPlatformError{errors.New(
			"keyboard or pointer source type not supported")}
	}
	inVardict := make(map[string]dbus.Variant)
	inVardict["session_handle_token"] = dbus.MakeVariant("t")
	result, outVardict, err := getResponse(bus, remoteDesktop,
		"org.freedesktop.portal.RemoteDesktop.CreateSession", 0, inVardict)
	if err != nil {
		return nil, UnsupportedPlatformError{err}
	}
	if result != 0 {
		return nil, UnsupportedPlatformError{errors.New(fmt.Sprintf(
			"Calling 'CreateSession' failed (%v)", result))}
	}
	sessionHandleV, ok := outVardict["session_handle"]
	if !ok {
		return nil, UnsupportedPlatformError{errors.New(
			"'session_handle' missing from 'CreateSession' return value")}
	}
	sessionHandleS, ok := sessionHandleV.Value().(string)
	if !ok {
		return nil, UnsupportedPlatformError{errors.New(
			"unexpected 'session_handle' type in 'CreateSession' return value")}
	}
	sessionHandle := dbus.ObjectPath(sessionHandleS)
	inVardict = make(map[string]dbus.Variant)
	inVardict["type"] = dbus.MakeVariant(deviceKeyboard | devicePointer)
	result, outVardict, err = getResponse(bus, remoteDesktop,
		"org.freedesktop.portal.RemoteDesktop.SelectDevices", 0, sessionHandle, inVardict)
	if err != nil {
		return nil, UnsupportedPlatformError{err}
	}
	if result != 0 {
		return nil, UnsupportedPlatformError{errors.New(fmt.Sprintf(
			"Calling 'SelectDevices' failed (%v)", result))}
	}
	inVardict = make(map[string]dbus.Variant)
	result, outVardict, err = getResponse(bus, remoteDesktop,
		"org.freedesktop.portal.RemoteDesktop.Start", 0, sessionHandle, "", inVardict)
	if err != nil {
		return nil, UnsupportedPlatformError{err}
	}
	if result != 0 {
		return nil, errors.New("keyboard or pointer access denied")
	}
	devicesV, ok := outVardict["devices"]
	if !ok {
		return nil, UnsupportedPlatformError{errors.New(
			"'devices' missing from 'Start' return value")}
	}
	devices, ok := devicesV.Value().(uint32)
	if !ok {
		return nil, UnsupportedPlatformError{errors.New(
			"unexpected 'devices' type in 'Start' return value")}
	}
	if devices&deviceKeyboard == 0 || devices&devicePointer == 0 {
		return nil, errors.New("keyboard or pointer access denied")
	}
	cleanupBus = false
	return &portalPlugin{bus: bus, remoteDesktop: remoteDesktop,
		sessionHandle: sessionHandle}, nil
}

func getResponse(bus *dbus.Conn, object dbus.BusObject, method string,
	flags dbus.Flags, args ...interface{}) (uint32, map[string]dbus.Variant, error) {
	ch := make(chan *dbus.Signal)
	bus.Signal(ch)
	defer bus.RemoveSignal(ch)
	var requestPath dbus.ObjectPath
	if err := object.Call(method, flags, args...).Store(&requestPath); err != nil {
		return 0, nil, err
	}
	for {
		s := <-ch
		if s.Path == requestPath && s.Name == "org.freedesktop.portal.Request.Response" {
			if len(s.Body) != 2 {
				return 0, nil, errors.New("unexpected 'Response' return length")
			}
			result, ok := s.Body[0].(uint32)
			if !ok {
				return 0, nil, errors.New("unexpected 'Response' return type")
			}
			outVardict, ok := s.Body[1].(map[string]dbus.Variant)
			if !ok {
				return 0, nil, errors.New("unexpected 'Response' return type")
			}
			return result, outVardict, nil
		}
	}
}

func (p *portalPlugin) Close() error {
	p.lock.Lock()
	defer p.lock.Unlock()
	if p.bus == nil {
		return errors.New("dbus connection closed")
	}
	if err := p.bus.Close(); err != nil {
		return err
	}
	p.bus = nil
	p.remoteDesktop = nil
	return nil
}

func (p *portalPlugin) KeyboardText(text string) error {
	p.lock.RLock()
	defer p.lock.RUnlock()
	if p.bus == nil {
		return errors.New("dbus connection closed")
	}
	inVardict := make(map[string]dbus.Variant)
	for _, runeValue := range text {
		keysym := int32(0x01000000 + runeValue)
		for _, state := range [...]uint32{btnPressed, btnReleased} {
			if err := p.remoteDesktop.Call(
				"org.freedesktop.portal.RemoteDesktop.NotifyKeyboardKeysym",
				0, p.sessionHandle, inVardict, keysym, state).Store(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *portalPlugin) PointerButton(button uint, press bool) error {
	p.lock.RLock()
	defer p.lock.RUnlock()
	if p.bus == nil {
		return errors.New("dbus connection closed")
	}
	var btn int32
	if button == 1 {
		btn = btnLeft
	} else if button == 2 {
		btn = btnMiddle
	} else if button == 3 {
		btn = btnRight
	} else {
		return errors.New("unsupported pointer button")
	}
	state := btnReleased
	if press {
		state = btnPressed
	}
	inVardict := make(map[string]dbus.Variant)
	if err := p.remoteDesktop.Call("org.freedesktop.portal.RemoteDesktop.NotifyPointerButton",
		0, p.sessionHandle, inVardict, btn, state).Store(); err != nil {
		return err
	}
	return nil
}

func (p *portalPlugin) PointerMove(deltaX, deltaY int) error {
	p.lock.RLock()
	defer p.lock.RUnlock()
	if p.bus == nil {
		return errors.New("dbus connection closed")
	}
	inVardict := make(map[string]dbus.Variant)
	if err := p.remoteDesktop.Call("org.freedesktop.portal.RemoteDesktop.NotifyPointerMotion",
		0, p.sessionHandle, inVardict, float64(deltaX), float64(deltaY)).Store(); err != nil {
		return err
	}
	return nil
}

func (p *portalPlugin) pointerScrollFull(deltaHorizontal, deltaVertical int, finish bool) error {
	p.lock.RLock()
	defer p.lock.RUnlock()
	if p.bus == nil {
		return errors.New("dbus connection closed")
	}
	inVardict := make(map[string]dbus.Variant)
	inVardict["finish"] = dbus.MakeVariant(finish)
	if err := p.remoteDesktop.Call("org.freedesktop.portal.RemoteDesktop.NotifyPointerAxis",
		0, p.sessionHandle, inVardict, float64(deltaHorizontal), float64(deltaVertical)).Store(); err != nil {
		return err
	}
	return nil
}

func (p *portalPlugin) PointerScroll(deltaHorizontal, deltaVertical int) error {
	return p.pointerScrollFull(deltaHorizontal, deltaVertical, false)
}

func (p *portalPlugin) PointerScrollFinish() error {
	return p.pointerScrollFull(0, 0, true)
}
