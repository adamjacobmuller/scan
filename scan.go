package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/hhrutter/pdfcpu/pkg/pdfcpu"
	"github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
)

func isBlank(img image.Image) (float64, image.Image, error) {
	bounds := img.Bounds()
	imgSet := image.NewRGBA(bounds)
	var blank, notBlank int
	for x := 0; x <= bounds.Max.X; x++ {
		for y := 0; y <= bounds.Max.Y; y++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if (r < 60000) || (g < 60000) || (b < 60000) {
				imgSet.Set(x, y, color.Gray{0})
				notBlank++
			} else {
				imgSet.Set(x, y, color.Gray{255})
				blank++
			}
		}
	}

	coverage := float64(float64(notBlank)/float64(bounds.Max.X*bounds.Max.Y)) * 100

	return coverage, imgSet, nil
}

func mainx() {
	fr, err := os.Open("in.jpg")
	if err != nil {
		panic(err)
	}
	img, err := jpeg.Decode(fr)
	if err != nil {
		panic(err)
	}

	_, imgSet, err := isBlank(img)
	if err != nil {
		panic(err)
	}
	outFile, err := os.Create("out.jpg")
	if err != nil {
		panic(err)
	}

	err = jpeg.Encode(outFile, imgSet, nil)
	if err != nil {
		panic(err)
	}

}

func mainy() {
	var err error
	err = process(
		log.WithFields(log.Fields{}),
		os.Args[1],
		"output.pdf",
	)
	if err != nil {
		panic(err)
	}
}
func main() {
	var err error
	directory := "/home/printer/incoming"
	files, err := ioutil.ReadDir(directory)
	if err != nil {
		panic(err)
	}
	for _, file := range files {
		if len(file.Name()) > 8 {
			if file.Name()[len(file.Name())-8:] == "-new.pdf" {
				continue
			}
		}
		if file.Name()[0:1] == "." {
			continue
		}
		if file.Name()[len(file.Name())-4:] != ".pdf" {
			log.WithFields(log.Fields{
				"file": file.Name(),
			}).Info("skipping non-pdf file")
			continue
		}

		fileId := uuid.Must(uuid.NewV4())

		log.WithFields(log.Fields{
			"file":   file.Name(),
			"fileId": fileId,
		}).Info("start processing pdf file")

		mt := file.ModTime()

		input := fmt.Sprintf("%s/%s", directory, file.Name())
		outDir := fmt.Sprintf("/home/printer/scans/%04d/%02d/%02d/%02d/%s", mt.Year(), mt.Month(), mt.Day(), mt.Hour(), fileId)
		output := fmt.Sprintf("%s/processed.pdf", outDir)

		err = os.MkdirAll(outDir, os.ModePerm)
		if err != nil {
			log.WithFields(log.Fields{
				"directory": outDir,
				"fileId":    fileId,
				"error":     err,
			}).Info("failed making directory")
			continue
		}

		newInput := fmt.Sprintf("%s/original.pdf", outDir)
		err = os.Rename(input, newInput)
		if err != nil {
			log.WithFields(log.Fields{
				"directory": outDir,
				"fileId":    fileId,
				"error":     err,
			}).Info("failed renaming file")
			continue
		}
		input = newInput

		err = process(
			log.WithFields(log.Fields{"file": file.Name(), "fileId": fileId}),
			input,
			output,
		)

		if err != nil {
			log.WithFields(log.Fields{
				"file":  file.Name(),
				"error": err,
			}).Info("failed processing pdf file")
			continue
		}
	}
}
func process(le *log.Entry, in, out string) error {
	ctx, err := pdfcpu.ReadPDFFile(in, nil)
	if err != nil {
		return err
	}

	err = pdfcpu.ValidateXRefTable(ctx.XRefTable)
	if err != nil {
		return err
	}

	err = pdfcpu.OptimizeXRefTable(ctx)
	if err != nil {
		return err
	}

	le.WithFields(log.Fields{
		"pages": ctx.PageCount,
		"size":  ctx.Size,
	}).Info("validated pdf")

	ctx.Write.ExtractPages = pdfcpu.IntSet{}

	for pageNumber := 1; pageNumber <= ctx.PageCount; pageNumber++ {
		//fmt.Printf("page(%d): %#v\n", pageNumber, ctx.Optimize.PageImages[pageNumber-1])
		le.WithFields(log.Fields{
			"page":   pageNumber,
			"images": ctx.Optimize.PageImages[pageNumber-1],
		}).Info("processing page")

		for imageNumber := range ctx.Optimize.PageImages[pageNumber-1] {
			le.WithFields(log.Fields{
				"page":  pageNumber,
				"image": imageNumber,
			}).Info("processing image")
			io, err := pdfcpu.ExtractImageData(ctx, imageNumber)
			if err != nil {
				return err
			}
			//fmt.Printf("page(%d)/img(%d)/io: %#v\n", pageNumber, imageNumber, io)

			buf := bytes.NewBuffer(io.Data())

			img, err := jpeg.Decode(buf)
			if err != nil {
				return err
			}
			le.WithFields(log.Fields{
				"page":   pageNumber,
				"image":  imageNumber,
				"bounds": img.Bounds(),
			}).Info("decoded jpeg")
			//fmt.Printf("page(%d)/img(%d)/image.Bounds(): %#v\n", pageNumber, imageNumber, img.Bounds())

			coverage, _, err := isBlank(img)
			if err != nil {
				return err
			}
			le.WithFields(log.Fields{
				"page":     pageNumber,
				"image":    imageNumber,
				"bounds":   img.Bounds(),
				"coverage": coverage,
			}).Info("calculated coverage")
			//fmt.Printf("page(%d)/img(%d)/coverage: %.4f%%\n", pageNumber, imageNumber, coverage)

			if coverage < 1 {
				ctx.Write.ExtractPages[pageNumber] = false
			} else {
				ctx.Write.ExtractPages[pageNumber] = true
			}
		}
	}

	//fmt.Printf("result: %+v\n", ctx.Write.ExtractPages)
	le.WithFields(log.Fields{
		"extractPages": ctx.Write.ExtractPages,
	}).Info("final ExtractPages map")

	ctx.Write.Command = "Trim"

	dirName, fileName := filepath.Split(out)
	ctx.Write.DirName = dirName
	ctx.Write.FileName = fileName

	err = pdfcpu.WritePDFFile(ctx)
	if err != nil {
		return err
	}

	return nil
}
