package i2c

import (
	"bytes"
	"encoding/binary"
	"time"

	"gobot.io/x/gobot"
)

const bmp180Address = 0x77

const bmp180RegisterAC1MSB = 0xAA

const bmp180RegisterCtl = 0xF4
const bmp180CmdTemp = 0x2E
const bmp180RegisterTempMSB = 0xF6
const bmp180CmdPressure = 0x34
const bmp180RegisterPressureMSB = 0xF6

// BMP180Driver is the gobot driver for the Bosch pressure sensor BMP180.
// Device datasheet: https://cdn-shop.adafruit.com/datasheets/BST-BMP180-DS000-09.pdf
type BMP180Driver struct {
	name       string
	connection I2c
	interval   time.Duration
	gobot.Eventer
	Pressure                float32
	Temperature             float32
	mode BMP180OversamplingMode
	calibrationCoefficients *calibrationCoefficients
}

// BMP180OversamplingMode is the oversampling ratio of the pressure measurement.
type BMP180OversamplingMode uint

const (
	// BMP180UltraLowPower is the lowest oversampling mode of the pressure measurement.
	BMP180UltraLowPower BMP180OversamplingMode = iota
	// BMP180Standard is the standard oversampling mode of the pressure measurement.
	BMP180Standard
	// BMP180HighResolution is a high oversampling mode of the pressure measurement.
	BMP180HighResolution
	// BMP180UltraHighResolution is the highest oversampling mode of the pressure measurement.
	BMP180UltraHighResolution
)	

type calibrationCoefficients struct {
	ac1 int16
	ac2 int16
	ac3 int16
	ac4 uint16
	ac5 uint16
	ac6 uint16
	b1  int16
	b2  int16
	mb  int16
	mc  int16
	md  int16
}

// NewBMP180Driver creates a new driver with the i2c interface for the BMP180 device.
func NewBMP180Driver(c I2c, mode BMP180OversamplingMode, i ...time.Duration) *BMP180Driver {
	d := &BMP180Driver{
		name:                    "BMP180",
		connection:              c,
		Eventer:                 gobot.NewEventer(),
		interval:                10 * time.Millisecond,
  	mode: mode,
		calibrationCoefficients: &calibrationCoefficients{},
	}

	if len(i) > 0 {
		d.interval = i[0]
	}
	d.AddEvent(Error)
	return d
}

// Name returns the name of the device.
func (d *BMP180Driver) Name() string {
	return d.name
}

// SetName sets the name of the device.
func (d *BMP180Driver) SetName(n string) {
	d.name = n
}

// Connection returns the connection of the device.
func (d *BMP180Driver) Connection() gobot.Connection {
	return d.connection.(gobot.Connection)
}

// Mode resturns the oversampling mode of the device.
func (d *BMP180Driver) Mode() BMP180OversamplingMode {
	return d.mode
}

// SetMode sets the oversampling mode of the device.
func (d *BMP180Driver) SetMode(mode BMP180OversamplingMode) {
	d.mode = mode
}

// Start writes initialization bytes and reads from adaptor
// using specified interval to load temperature and pressure data.
func (d *BMP180Driver) Start() (err error) {
	var rawTemp int16
	var rawPressure int32
	if err := d.initialization(); err != nil {
		return err
	}
	go func() {
		for {
			if rawTemp, err = d.rawTemp(); err != nil {
				d.Publish(d.Event(Error), err)
				continue
			}
			d.Temperature = d.calculateTemp(rawTemp)
			if rawPressure, err = d.rawPressure(); err != nil {
				d.Publish(d.Event(Error), err)
				continue
			}
			d.Pressure = d.calculatePressure(rawTemp, rawPressure)
			time.Sleep(d.interval)
		}
	}()
	return
}

func (d *BMP180Driver) initialization() (err error) {
	if err = d.connection.I2cStart(bmp180Address); err != nil {
		return err
	}
	var coefficients []byte
	// read the 11 calibration coefficients.
	if coefficients, err = d.read(bmp180RegisterAC1MSB, 22); err != nil {
		return err
	}
	buf := bytes.NewBuffer(coefficients)
	binary.Read(buf, binary.BigEndian, &d.calibrationCoefficients.ac1)
	binary.Read(buf, binary.BigEndian, &d.calibrationCoefficients.ac2)
	binary.Read(buf, binary.BigEndian, &d.calibrationCoefficients.ac3)
	binary.Read(buf, binary.BigEndian, &d.calibrationCoefficients.ac4)
	binary.Read(buf, binary.BigEndian, &d.calibrationCoefficients.ac5)
	binary.Read(buf, binary.BigEndian, &d.calibrationCoefficients.ac6)	
	binary.Read(buf, binary.BigEndian, &d.calibrationCoefficients.b1)
	binary.Read(buf, binary.BigEndian, &d.calibrationCoefficients.b2)
	binary.Read(buf, binary.BigEndian, &d.calibrationCoefficients.mb)
	binary.Read(buf, binary.BigEndian, &d.calibrationCoefficients.mc)
	binary.Read(buf, binary.BigEndian, &d.calibrationCoefficients.md)
	return nil
}

func (d *BMP180Driver) rawTemp() (int16, error) {
	if err := d.connection.I2cWrite(bmp180Address, []byte{bmp180RegisterCtl, bmp180CmdTemp}); err != nil {
		return 0, err
	}
	time.Sleep(5 * time.Millisecond)
	ret, err := d.read(bmp180RegisterTempMSB, 2)
	if err != nil {
		return 0, err
	}
	buf := bytes.NewBuffer(ret)
	var rawTemp int16
	binary.Read(buf, binary.BigEndian, &rawTemp)
	return rawTemp, nil
}

func (d *BMP180Driver) read(address byte, n int) ([]byte, error) {
	if err := d.connection.I2cWrite(bmp180Address, []byte{address}); err != nil {
		return nil, err
	}
	ret, err := d.connection.I2cRead(bmp180Address, n)
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func (d *BMP180Driver) calculateTemp(rawTemp int16) float32 {
	b5 := d.calculateB5(rawTemp)
	t := (b5 + 8) >> 4
	return float32(t) / 10
}

func (d *BMP180Driver) calculateB5(rawTemp int16) int32 {
	x1 := (int32(rawTemp) -  int32(d.calibrationCoefficients.ac6)) * int32(d.calibrationCoefficients.ac5) >> 15
	x2 := int32(d.calibrationCoefficients.mc) << 11 / (x1 + int32(d.calibrationCoefficients.md))
	return x1 + x2
}

func (d *BMP180Driver) rawPressure() (rawPressure int32, err error) {
	if err := d.connection.I2cWrite(bmp180Address, []byte{bmp180RegisterCtl, bmp180CmdPressure + byte(d.mode<<6)}); err != nil {
		return 0, err
	}
	switch(d.mode) {
	case BMP180UltraLowPower:
		time.Sleep(5 * time.Millisecond)
	case BMP180Standard:
		time.Sleep(8 * time.Millisecond)
	case BMP180HighResolution:
		time.Sleep(14 * time.Millisecond)
	case BMP180UltraHighResolution:
		time.Sleep(26 * time.Millisecond)
	}
	var ret []byte
	if ret, err = d.read(bmp180RegisterPressureMSB, 3); err != nil {
		return 0, err
	}
	rawPressure = (int32(ret[0]) << 16 + int32(ret[1]) << 8 + int32(ret[2])) >> (8 - uint(d.mode))
	return rawPressure, nil
}

func (d *BMP180Driver) calculatePressure(rawTemp int16, rawPressure int32) float32 {
	b5 := d.calculateB5(rawTemp)
	b6 := b5 - 4000
	x1 := (int32(d.calibrationCoefficients.b2) * (b6 * b6 >> 12)) >> 11
	x2 := (int32(d.calibrationCoefficients.ac2) * b6) >> 11
	x3 := x1 + x2
	b3 := (((int32(d.calibrationCoefficients.ac1) * 4 + x3) << uint(d.mode)) + 2) >> 2
	x1 = (int32(d.calibrationCoefficients.ac3) * b6) >> 13
	x2 = (int32(d.calibrationCoefficients.b1) * ((b6 * b6) >> 12)) >> 16 
  x3 = ((x1 + x2) + 2) >> 2
	b4 := (uint32(d.calibrationCoefficients.ac4) * uint32(x3 + 32768)) >> 15
	b7 := (uint32(rawPressure - b3) * (50000 >> uint(d.mode)))
	var p int32
  if (b7 < 0x80000000) {
		p = int32((b7 << 1) / b4)
	} else {
		p = int32((b7 / b4) << 1)
	}
	x1 = (p >> 8) * (p >> 8)
  x1 = (x1 * 3038) >> 16
  x2 = (-7357 * p) >> 16
  return float32(p + ((x1 + x2 + 3791) >> 4))
}

// Halt halts the device.
func (d *BMP180Driver) Halt() (err error) {
	return nil
}
