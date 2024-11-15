package main

import (
	"errors"
	"io/ioutil"
	"log"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/enums"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
)

// Be sure to close pools/instances when you're done with them.
var pool pdfium.Pool

func init() {
	var err error

	// Init the PDFium library and return the instance to open documents.
	// You can tweak these configs to your need. Be aware that workers can use quite some memory.
	pool, err = webassembly.Init(webassembly.Config{
		MinIdle:  3,  // Makes sure that at least x workers are always available
		MaxIdle:  3,  // Makes sure that at most x workers are ever available
		MaxTotal: 32, // Maxium amount of workers in total, allows the amount of workers to grow when needed, items between total max and idle max are automatically cleaned up, while idle workers are kept alive so they can be used directly.
	})
	if err != nil {
		log.Fatal(err)
	}

}

func main() {
	filePath := "../../shared_tests/testdata/test.pdf"

	var wg sync.WaitGroup
	for i := 0; i < 128; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := renderPages(filePath); err != nil {
				log.Fatalln(err)
			}
		}()
	}
	wg.Wait()

	if err := pool.Close(); err != nil {
		log.Fatal(err)
	}
}

func renderPages(filePath string) error {
	instance, err := pool.GetInstance(time.Second * 30)
	if err != nil {
		return err
	}

	defer instance.Close()

	pdfBytes, err := ioutil.ReadFile(filePath)
	if err != nil {
		return err
	}

	doc, err := instance.OpenDocument(&requests.OpenDocument{
		File: &pdfBytes,
	})
	if err != nil {
		return err
	}

	defer instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{
		Document: doc.Document,
	})

	pageCount, err := instance.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return err
	}

	for i := 0; i < pageCount.PageCount; i++ {
		err := func() error {
			page, err := instance.FPDF_LoadPage(&requests.FPDF_LoadPage{
				Document: doc.Document,
				Index:    i,
			})
			if err != nil {
				return err
			}

			defer instance.FPDF_ClosePage(&requests.FPDF_ClosePage{Page: page.Page})

			pageSize, err := instance.GetPageSizeInPixels(&requests.GetPageSizeInPixels{
				Page: requests.Page{ByReference: &page.Page},
				DPI:  72,
			})
			if err != nil {
				return err
			}

			bmp, err := instance.FPDFBitmap_Create(&requests.FPDFBitmap_Create{
				Alpha:  1,
				Height: pageSize.Height,
				Width:  pageSize.Width,
			})
			if err != nil {
				return err
			}

			defer instance.FPDFBitmap_Destroy(&requests.FPDFBitmap_Destroy{Bitmap: bmp.Bitmap})

			callback := func() bool { return true }

			renderStart, err := instance.FPDF_RenderPageBitmap_Start(&requests.FPDF_RenderPageBitmap_Start{
				Bitmap:                 bmp.Bitmap,
				Page:                   requests.Page{ByReference: &page.Page},
				SizeX:                  pageSize.Width,
				SizeY:                  pageSize.Height,
				NeedToPauseNowCallback: callback,
			})
			if err != nil {
				return err
			}

			defer instance.FPDF_RenderPage_Close(&requests.FPDF_RenderPage_Close{
				Page: requests.Page{ByReference: &page.Page},
			})

			status := renderStart.RenderStatus
			for {
				switch status {
				case enums.FPDF_RENDER_STATUS_TOBECONTINUED:
					resp, err := instance.FPDF_RenderPage_Continue(&requests.FPDF_RenderPage_Continue{
						Page:                   requests.Page{ByReference: &page.Page},
						NeedToPauseNowCallback: callback,
					})
					if err != nil {
						return err
					}

					status = resp.RenderStatus
				case enums.FPDF_RENDER_STATUS_FAILED:
					return errors.New("Render Failed")
				case enums.FPDF_RENDER_STATUS_DONE:
					log.Printf("Rendered page at index %d\n", i)
					return nil
				}
			}
		}()
		if err != nil {
			return err
		}
	}

	return nil
}
