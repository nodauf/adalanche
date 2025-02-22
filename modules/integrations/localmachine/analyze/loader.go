package analyze

import (
	"io/ioutil"
	"runtime"
	"strings"
	"sync"

	"github.com/lkarlslund/adalanche/modules/engine"
	"github.com/lkarlslund/adalanche/modules/integrations/activedirectory"
	"github.com/lkarlslund/adalanche/modules/integrations/localmachine"
	"github.com/lkarlslund/adalanche/modules/windowssecurity"
	"github.com/mailru/easyjson"
	"github.com/rs/zerolog/log"
)

var (
	myloader CollectorLoader
	// dscollector = engine.AttributeValueString(myloader.Name())
)

func init() {
	engine.AddLoader(&myloader)
}

type CollectorLoader struct {
	done       sync.WaitGroup
	ao         *engine.Objects
	infostoadd chan string
}

func (ld *CollectorLoader) Name() string {
	return "Collector JSON file"
}

func (ld *CollectorLoader) Init() error {
	ld.ao = engine.NewLoaderObjects(ld)
	ld.ao.SetThreadsafe(true)

	ld.infostoadd = make(chan string, 128)

	for i := 0; i < runtime.NumCPU(); i++ {
		ld.done.Add(1)
		go func() {
			for path := range ld.infostoadd {
				raw, err := ioutil.ReadFile(path)
				if err != nil {
					log.Warn().Msgf("Problem reading data from JSON file %v: %v", path, err)
					continue
				}

				var cinfo localmachine.Info
				err = easyjson.Unmarshal(raw, &cinfo)
				if err != nil {
					log.Warn().Msgf("Problem unmarshalling data from JSON file %v: %v", path, err)
					continue
				}

				// ld.infoaddmutex.Lock()
				err = ImportCollectorInfo(cinfo, ld.ao)
				if err != nil {
					log.Warn().Msgf("Problem importing collector info: %v", err)
					continue
				}

				// ld.ao.AddMerge([]engine.Attribute{engine.ObjectSid}, generatedobjs...)
				// ld.infoaddmutex.Unlock()
			}
			ld.done.Done()
		}()
	}

	return nil
}

func (ld *CollectorLoader) Close() ([]*engine.Objects, error) {
	close(ld.infostoadd)
	ld.done.Wait()
	ld.ao.SetThreadsafe(false)

	for _, o := range ld.ao.Slice() {
		if o.HasAttr(activedirectory.ObjectSid) {

			// We can do this with confidence as everything comes from this loader
			sidwithoutrid := o.OneAttrRaw(activedirectory.ObjectSid).(windowssecurity.SID).StripRID()

			switch o.Type() {
			case engine.ObjectTypeComputer:
				// We don't link that - it's either absorbed into the real computer object, or it's orphaned
			case engine.ObjectTypeUser:
				// It's a User we added, find the computer
				if computer, found := ld.ao.Find(LocalMachineSID, engine.AttributeValueSID(sidwithoutrid)); found {
					o.ChildOf(computer) // FIXME -> Users
				}
			case engine.ObjectTypeGroup:
				// It's a Group we added
				if computer, found := ld.ao.Find(LocalMachineSID, engine.AttributeValueSID(sidwithoutrid)); found {
					o.ChildOf(computer) // FIXME -> Groups
				}
			default:
				if o.HasAttr(activedirectory.ObjectSid) {
					if computer, found := ld.ao.Find(LocalMachineSID, engine.AttributeValueSID(sidwithoutrid)); found {
						o.ChildOf(computer) // We don't know what it is
					}
				}
			}
		}
	}

	return []*engine.Objects{ld.ao}, nil
}

func (ld *CollectorLoader) Load(path string, cb engine.ProgressCallbackFunc) error {
	if !strings.HasSuffix(path, localmachine.Suffix) {
		return engine.ErrUninterested
	}

	ld.infostoadd <- path
	return nil
}
