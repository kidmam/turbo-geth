package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wcharczuk/go-chart"
	"github.com/wcharczuk/go-chart/drawing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
)

var chartColors = []drawing.Color{
	chart.ColorBlack,
	chart.ColorRed,
	chart.ColorBlue,
	chart.ColorYellow,
	chart.ColorOrange,
	chart.ColorGreen,
}

func runBlock(tds *state.TrieDbState, dbstate *state.Stateless, chainConfig *params.ChainConfig,
	bcb core.ChainContext, header *types.Header, block *types.Block, trace bool, checkRoot bool,
) error {
	vmConfig := vm.Config{}
	engine := ethash.NewFullFaker()
	statedb := state.New(dbstate)
	statedb.SetTrace(trace)
	gp := new(core.GasPool).AddGas(block.GasLimit())
	usedGas := new(uint64)
	var receipts types.Receipts
	if chainConfig.DAOForkSupport && chainConfig.DAOForkBlock != nil && chainConfig.DAOForkBlock.Cmp(block.Number()) == 0 {
		misc.ApplyDAOHardFork(statedb)
	}
	for _, tx := range block.Transactions() {
		receipt, _, err := core.ApplyTransaction(chainConfig, bcb, nil, gp, statedb, dbstate, header, tx, usedGas, vmConfig)
		if err != nil {
			return fmt.Errorf("tx %x failed: %v", tx.Hash(), err)
		}
		receipts = append(receipts, receipt)
	}
	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	if _, err := engine.Finalize(chainConfig, header, statedb, block.Transactions(), block.Uncles(), receipts); err != nil {
		return fmt.Errorf("Finalize of block %d failed: %v", block.NumberU64(), err)
	}
	dbstate.SetBlockNr(block.NumberU64())
	if err := statedb.Commit(chainConfig.IsEIP158(header.Number), dbstate); err != nil {
		return fmt.Errorf("Commiting block %d failed: %v", block.NumberU64(), err)
	}
	if err := dbstate.CheckRoot(header.Root, checkRoot); err != nil {
		filename := fmt.Sprintf("right_%d.txt", block.NumberU64())
		f, err1 := os.Create(filename)
		if err1 == nil {
			defer f.Close()
			tds.PrintTrie(f)
		}
		return fmt.Errorf("Error processing block %d: %v", block.NumberU64(), err)
	}
	return nil
}

func writeStats(w io.Writer, blockNum uint64, blockProof state.BlockProof) {
	var totalCShorts, totalCValues, totalCodes, totalShorts, totalValues int
	for _, short := range blockProof.CShortKeys {
		l := len(short)
		if short[l-1] == 16 {
			l -= 1
		}
		l = l/2 + 1
		totalCShorts += l
	}
	for _, value := range blockProof.CValues {
		totalCValues += len(value)
	}
	for _, code := range blockProof.Codes {
		totalCodes += len(code)
	}
	for _, short := range blockProof.ShortKeys {
		l := len(short)
		if short[l-1] == 16 {
			l -= 1
		}
		l = l/2 + 1
		totalShorts += l
	}
	for _, value := range blockProof.Values {
		totalValues += len(value)
	}
	fmt.Fprintf(w, "%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d\n",
		blockNum, len(blockProof.Contracts), len(blockProof.CMasks), len(blockProof.CHashes), len(blockProof.CShortKeys), len(blockProof.CValues), len(blockProof.Codes),
		len(blockProof.Masks), len(blockProof.Hashes), len(blockProof.ShortKeys), len(blockProof.Values), totalCShorts, totalCValues, totalCodes, totalShorts, totalValues,
	)
}

func stateless(genLag, consLag int) {
	//state.MaxTrieCacheGen = 64*1024
	startTime := time.Now()
	sigs := make(chan os.Signal, 1)
	interruptCh := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		interruptCh <- true
	}()

	ethDb, err := ethdb.NewLDBDatabase("/Volumes/tb4/turbo-geth-copy/geth/chaindata")
	//ethDb, err := ethdb.NewLDBDatabase("/Users/alexeyakhunov/Library/Ethereum/geth/chaindata")
	//ethDb, err := ethdb.NewLDBDatabase("/home/akhounov/.ethereum/geth/chaindata1")
	check(err)
	defer ethDb.Close()
	chainConfig := params.MainnetChainConfig
	//slFile, err := os.OpenFile("/Volumes/tb4/turbo-geth/stateless.csv", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	slFile, err := os.OpenFile("stateless.csv", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	check(err)
	defer slFile.Close()
	w := bufio.NewWriter(slFile)
	defer w.Flush()
	slfFile, err := os.OpenFile(fmt.Sprintf("stateless_%d.csv", consLag), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	check(err)
	defer slfFile.Close()
	wf := bufio.NewWriter(slfFile)
	vmConfig := vm.Config{}
	engine := ethash.NewFullFaker()
	bcb, err := core.NewBlockChain(ethDb, nil, chainConfig, engine, vm.Config{}, nil)
	check(err)
	stateDb, db := ethdb.NewMemDatabase2()
	defer stateDb.Close()
	blockNum := uint64(*block)
	var preRoot common.Hash
	if blockNum == 1 {
		_, _, _, err = core.SetupGenesisBlock(stateDb, core.DefaultGenesisBlock())
		check(err)
		genesisBlock, _, _, err := core.DefaultGenesisBlock().ToBlock(nil)
		check(err)
		preRoot = genesisBlock.Header().Root
	} else {
		load_snapshot(db, fmt.Sprintf("state_%d", blockNum-1))
		load_codes(db, ethDb)
		block := bcb.GetBlockByNumber(blockNum - 1)
		fmt.Printf("Block number: %d\n", blockNum-1)
		fmt.Printf("Block root hash: %x\n", block.Root())
		preRoot = block.Root()
		check_roots(stateDb, db, preRoot, blockNum-1)
	}
	batch := stateDb.NewBatch()
	tds, err := state.NewTrieDbState(preRoot, batch, blockNum-1)
	check(err)
	if blockNum > 1 {
		tds.Rebuild()
	}
	tds.SetResolveReads(false)
	tds.SetNoHistory(true)
	interrupt := false
	var thresholdBlock uint64 = 0
	//prev := make(map[uint64]*state.Stateless)
	var proofGen *state.Stateless  // Generator of proofs
	var proofCons *state.Stateless // Consumer of proofs
	for !interrupt {
		trace := blockNum == 2509930
		if trace {
			filename := fmt.Sprintf("right_%d.txt", blockNum-1)
			f, err1 := os.Create(filename)
			if err1 == nil {
				defer f.Close()
				tds.PrintTrie(f)
				//tds.PrintStorageTrie(f, common.HexToHash("1a4fa162e70315921486693f1d5943b7704232081b39206774caa567d63f633f"))
			}
		}
		tds.SetResolveReads(blockNum >= thresholdBlock)
		block := bcb.GetBlockByNumber(blockNum)
		if block == nil {
			break
		}
		statedb := state.New(tds)
		gp := new(core.GasPool).AddGas(block.GasLimit())
		usedGas := new(uint64)
		header := block.Header()
		var receipts types.Receipts
		if chainConfig.DAOForkSupport && chainConfig.DAOForkBlock != nil && chainConfig.DAOForkBlock.Cmp(block.Number()) == 0 {
			misc.ApplyDAOHardFork(statedb)
		}
		for i, tx := range block.Transactions() {
			statedb.Prepare(tx.Hash(), block.Hash(), i)
			receipt, _, err := core.ApplyTransaction(chainConfig, bcb, nil, gp, statedb, tds.TrieStateWriter(), header, tx, usedGas, vmConfig)
			if err != nil {
				fmt.Printf("tx %x failed: %v\n", tx.Hash(), err)
				return
			}
			if !chainConfig.IsByzantium(header.Number) {
				//rootHash, err := tds.TrieRoot()
				//if err != nil {
				//	panic(fmt.Errorf("tx %d, %x failed: %v", i, tx.Hash(), err))
				//}
				//receipt.PostState = rootHash.Bytes()
			}
			receipts = append(receipts, receipt)
		}
		// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
		_, err = engine.Finalize(chainConfig, header, statedb, block.Transactions(), block.Uncles(), receipts)
		if err != nil {
			fmt.Printf("Finalize of block %d failed: %v\n", blockNum, err)
			return
		}
		nextRoot, err := tds.IntermediateRoot(statedb, chainConfig.IsEIP158(header.Number))
		if err != nil {
			fmt.Printf("Failed to calculate IntermediateRoot: %v\n", err)
			return
		}
		//fmt.Printf("Next root %x\n", nextRoot)
		if nextRoot != block.Root() {
			fmt.Printf("Root hash does not match for block %d, expected %x, was %x\n", blockNum, block.Root(), nextRoot)
		}
		tds.SetBlockNr(blockNum)
		err = statedb.Commit(chainConfig.IsEIP158(header.Number), tds.DbStateWriter())
		if err != nil {
			fmt.Errorf("Commiting block %d failed: %v", blockNum, err)
			return
		}
		if _, err := batch.Commit(); err != nil {
			fmt.Printf("Failed to commit batch: %v\n", err)
			return
		}
		if (blockNum%500000 == 0) || (blockNum > 5600000 && blockNum%100000 == 0) {
			save_snapshot(db, fmt.Sprintf("/Volumes/tb4/turbo-geth-copy/state_%d", blockNum))
		}
		if blockNum >= thresholdBlock {
			blockProof := tds.ExtractProofs(trace)
			dbstate, err := state.NewStateless(preRoot, blockProof, block.NumberU64()-1, trace)
			if err != nil {
				fmt.Printf("Error making state for block %d: %v\n", blockNum, err)
			} else {
				if err := runBlock(tds, dbstate, chainConfig, bcb, header, block, trace, true); err != nil {
					fmt.Printf("Error running block %d through stateless0: %v\n", blockNum, err)
				} else {
					writeStats(w, blockNum, blockProof)
				}
			}
			if proofCons == nil {
				proofCons, err = state.NewStateless(preRoot, blockProof, block.NumberU64()-1, false)
				if err != nil {
					fmt.Printf("Error making proof consumer for block %d: %v\n", blockNum, err)
				}
			}
			if proofGen == nil {
				proofGen, err = state.NewStateless(preRoot, blockProof, block.NumberU64()-1, false)
				if err != nil {
					fmt.Printf("Error making proof generator for block %d: %v\n", blockNum, err)
				}
			}
			if proofGen != nil && proofCons != nil {
				if blockNum > uint64(consLag) {
					pBlockProof := proofGen.ThinProof(blockProof, block.NumberU64()-1, blockNum-uint64(consLag), trace)
					if err := proofCons.ApplyProof(preRoot, pBlockProof, block.NumberU64()-1, false); err != nil {
						fmt.Printf("Error applying thin proof to consumer: %v\n", err)
						return
					}
					writeStats(wf, blockNum, pBlockProof)
					proofCons.Prune(blockNum-uint64(consLag), false)
				} else {
					if err := proofCons.ApplyProof(preRoot, blockProof, block.NumberU64()-1, false); err != nil {
						fmt.Printf("Error applying proof to consumer: %v\n", err)
						return
					}
				}
				if err := runBlock(tds, proofCons, chainConfig, bcb, header, block, trace, false); err != nil {
					fmt.Printf("Error running block %d through proof consumer: %v\n", blockNum, err)
				}
			}
			if proofGen != nil {
				if err := proofGen.ApplyProof(preRoot, blockProof, block.NumberU64()-1, false); err != nil {
					fmt.Printf("Error applying proof to generator: %v\n", err)
					return
				}
				if err := runBlock(tds, proofGen, chainConfig, bcb, header, block, trace, false); err != nil {
					fmt.Printf("Error running block %d through proof generator: %v\nn", blockNum, err)
				}
				if blockNum > uint64(genLag) {
					proofGen.Prune(blockNum-uint64(genLag), false)
				}
			}
		}
		preRoot = header.Root
		blockNum++
		if blockNum%1000 == 0 {
			tds.PruneTries(true)
			fmt.Printf("Processed %d blocks\n", blockNum)
		}
		// Check for interrupts
		select {
		case interrupt = <-interruptCh:
			fmt.Println("interrupted, please wait for cleanup...")
		default:
		}
	}
	fmt.Printf("Processed %d blocks\n", blockNum)
	fmt.Printf("Next time specify -block %d\n", blockNum)
	fmt.Printf("Stateless client analysis took %s\n", time.Since(startTime))
}

func stateless_chart_key_values(filename string, right []int, chartFileName string, start int, startColor int) {
	file, err := os.Open(filename)
	check(err)
	defer file.Close()
	reader := csv.NewReader(bufio.NewReader(file))
	var blocks []float64
	var vals [18][]float64
	count := 0
	for records, _ := reader.Read(); records != nil; records, _ = reader.Read() {
		count++
		if count < start {
			continue
		}
		blocks = append(blocks, parseFloat64(records[0])/1000000.0)
		for i := 0; i < 18; i++ {
			cProofs := 4.0*parseFloat64(records[2]) + 32.0*parseFloat64(records[3]) + parseFloat64(records[11]) + parseFloat64(records[12])
			proofs := 4.0*parseFloat64(records[7]) + 32.0*parseFloat64(records[8]) + parseFloat64(records[14]) + parseFloat64(records[15])
			switch i {
			case 1, 6:
				vals[i] = append(vals[i], 4.0*parseFloat64(records[i+1]))
			case 2, 7:
				vals[i] = append(vals[i], 32.0*parseFloat64(records[i+1]))
			case 15:
				vals[i] = append(vals[i], cProofs)
			case 16:
				vals[i] = append(vals[i], proofs)
			case 17:
				vals[i] = append(vals[i], cProofs+proofs+parseFloat64(records[13]))
			default:
				vals[i] = append(vals[i], parseFloat64(records[i+1]))
			}
		}
	}
	var windowSums [18]float64
	var window int = 1024
	var movingAvgs [18][]float64
	for i := 0; i < 18; i++ {
		movingAvgs[i] = make([]float64, len(blocks)-(window-1))
	}
	for j := 0; j < len(blocks); j++ {
		for i := 0; i < 18; i++ {
			windowSums[i] += vals[i][j]
		}
		if j >= window {
			for i := 0; i < 18; i++ {
				windowSums[i] -= vals[i][j-window]
			}
		}
		if j >= window-1 {
			for i := 0; i < 18; i++ {
				movingAvgs[i][j-window+1] = windowSums[i] / float64(window)
			}
		}
	}
	movingBlock := blocks[window-1:]
	seriesNames := [18]string{
		"Number of contracts",
		"Contract masks",
		"Contract hashes",
		"Number of contract leaf keys",
		"Number of contract leaf vals",
		"Number of contract codes",
		"Masks",
		"Hashes",
		"Number of leaf keys",
		"Number of leaf values",
		"Total size of contract leaf keys",
		"Total size of contract leaf vals",
		"Total size of codes",
		"Total size of leaf keys",
		"Total size of leaf vals",
		"Block proofs (contracts only)",
		"Block proofs (without contracts)",
		"Block proofs (total)",
	}
	var currentColor int = startColor
	var series []chart.Series
	for _, r := range right {
		s := &chart.ContinuousSeries{
			Name: seriesNames[r],
			Style: chart.Style{
				Show:        true,
				StrokeColor: chartColors[currentColor],
				//FillColor:   chartColors[currentColor].WithAlpha(100),
			},
			XValues: movingBlock,
			YValues: movingAvgs[r],
		}
		currentColor++
		series = append(series, s)
	}

	graph1 := chart.Chart{
		Width:  1280,
		Height: 720,
		Background: chart.Style{
			Padding: chart.Box{
				Top: 50,
			},
		},
		YAxis: chart.YAxis{
			Name:      "kBytes",
			NameStyle: chart.StyleShow(),
			Style:     chart.StyleShow(),
			TickStyle: chart.Style{
				TextRotationDegrees: 45.0,
			},
			ValueFormatter: func(v interface{}) string {
				return fmt.Sprintf("%d kB", int(v.(float64)/1024.0))
			},
			GridMajorStyle: chart.Style{
				Show:        true,
				StrokeColor: chart.ColorBlack,
				StrokeWidth: 1.0,
			},
			//GridLines: days(),
		},
		/*
			YAxisSecondary: chart.YAxis{
				NameStyle: chart.StyleShow(),
				Style: chart.StyleShow(),
				TickStyle: chart.Style{
					TextRotationDegrees: 45.0,
				},
				ValueFormatter: func(v interface{}) string {
					return fmt.Sprintf("%d", int(v.(float64)))
				},
			},
		*/
		XAxis: chart.XAxis{
			Name: "Blocks, million",
			Style: chart.Style{
				Show: true,
			},
			ValueFormatter: func(v interface{}) string {
				return fmt.Sprintf("%.3fm", v.(float64))
			},
			GridMajorStyle: chart.Style{
				Show:        true,
				StrokeColor: chart.ColorAlternateGray,
				StrokeWidth: 1.0,
			},
			//GridLines: blockMillions(),
		},
		Series: series,
	}

	graph1.Elements = []chart.Renderable{chart.LegendThin(&graph1)}

	buffer := bytes.NewBuffer([]byte{})
	err = graph1.Render(chart.PNG, buffer)
	check(err)
	err = ioutil.WriteFile(chartFileName, buffer.Bytes(), 0644)
	check(err)
}
