package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"sort"
	"time"
)

var (
	crossoverRate         float64
	mutationRate          float64
	amountOfIndividuals   int
	amountOfEntities      int
	maxGenerations        int
	childrenPerGeneration int
	desiredFitness        float64
	debug                 bool
	stopAtFirstPerfect    bool
	esHost                string
	esPort                string
	esIndex               string
)

func init() {
	flag.Float64Var(&crossoverRate, "cor", 1.0, "TODO: Crossover rate. Chance of crossover in every iteration.")
	flag.Float64Var(&mutationRate, "mr", 0.1, "Mutation rate. Chance of mutation of children after birth.")
	flag.IntVar(&amountOfIndividuals, "nri", 10, "Number of individuals.")
	flag.IntVar(&amountOfEntities, "nre", 32, "Number of entities.")
	flag.IntVar(&maxGenerations, "maxgen", 200000, "Generations to try before break.")
	flag.IntVar(&childrenPerGeneration, "chldpg", 10, "Number of children produced in every generation.")
	flag.Float64Var(&desiredFitness, "ftnss", 32, "Fitness we try to reach.")
	flag.BoolVar(&debug, "debug", false, "Enable debug output")
	flag.BoolVar(&stopAtFirstPerfect, "stop", true, "Stop at first sight of a perfect individual.")
	flag.StringVar(&esHost, "eshost", "localhost", "Hostname/IP of ElasticSearch.")
	flag.StringVar(&esPort, "esport", "9200", "Port of ElasticSearch.")
	flag.StringVar(&esIndex, "esindex", "logstash-ec", "Index name for ElasticSearch.")
}

// Sort by fitness
type byFitness []individual

func (v byFitness) Len() int           { return len(v) }
func (v byFitness) Swap(i, j int)      { v[i], v[j] = v[j], v[i] }
func (v byFitness) Less(i, j int) bool { return v[i].fitness < v[j].fitness }

// Lowercase makes it only visible to the current package
type population struct {
	individuals    []individual
	maxIndividuals int
	maxFitness     int
	avgFitness     float64
}

type individual struct {
	entities   []int
	amount     int
	fitness    int
	generation int
}

type DbIndividual struct {
	Entities   []int `json:"entities"`
	Amount     int   `json:"amount"`
	Fitness    int   `json:"fitness"`
	Generation int   `json:"generation"`
}

func (p *population) Individuals() []individual {
	return p.individuals
}

func (p *population) TwoIndividual(x, y int) (individual, individual) {
	return p.individuals[x], p.individuals[y]
}

func (p *population) ReplaceIndividuals(x, xi, y, yi individual) {
	replacePosX, replacePosY := p.Pos(x), p.Pos(y)
	p.SetIndividual(replacePosX, xi)
	p.SetIndividual(replacePosY, yi)
}

func (p *population) SetIndividual(x int, xi individual) {
	p.individuals[x] = xi
}

func (p *population) AppendIndividual(i individual) {
	p.individuals = append(p.individuals, i)
}

func (p *population) AppendIndividuals(i, j individual) {
	p.individuals = append(p.individuals, i)
	p.individuals = append(p.individuals, j)
}

func (p *population) Pos(i individual) int {
	for k, v := range p.Individuals() {
		if testEq(v.Entities(), i.Entities()) {
			return k
		}
	}
	return -1
}

func (p *population) AvgFitness() float64 {
	return p.avgFitness
}

func (p *population) RefreshAvgFitness() {
	avgFitness := float64(0)
	individuals := p.Individuals()
	for i := range individuals {
		fit := fitness(individuals[i].entities)
		if stopAtFirstPerfect && fit == p.maxFitness {
			fmt.Println("Winner:", individuals[i])
			os.Exit(0)
		}
		avgFitness += float64(fit)
	}
	avgFitness = avgFitness / float64(len(p.individuals))
	p.avgFitness = avgFitness
}

func (p *population) RemoveIndividual(i int) {
	p.individuals = append(p.individuals[:i], p.individuals[i+1:]...)
}

func (p *population) RefreshFitness() {
	for i := range p.individuals {
		individual := p.Individuals()
		individual[i].fitness = fitness(individual[i].entities)
	}
}

func (p *population) RefreshGeneration(gen int) {
	for i := range p.individuals {
		individual := p.Individuals()
		individual[i].generation = gen
	}
}

func (p *population) PrintAll() {
	for i := range p.individuals {
		individual := p.Individuals()
		fmt.Println(i, individual[i])
	}
}

func (p *population) KillWeak() {
	// fmt.Println(p.individuals)
	sort.Sort(byFitness(p.individuals))
	p.individuals = append(p.individuals[len(p.individuals)-amountOfIndividuals:])
	// fmt.Println(p.individuals)
	p.RefreshAvgFitness()
}

func (p *population) Initialize(amount, length int) {
	// Creates "amount" individuals with "length" entities
	p.maxIndividuals = amount
	p.maxFitness = length
	for i := 0; i < amount; i++ {
		randomVals := createRandomValues(length)
		appendMe := individual{randomVals, length, fitness(randomVals), 0}
		p.AppendIndividual(appendMe)
	}
	fmt.Println("Initialized w/ random:", p.individuals)
}

func (p *population) ParentSelection() (individual, individual) {
	// Returns two random individuals which will be the parents
	length := len(p.individuals)
	mother := rand.Intn(length)
	father := rand.Intn(length)
	for {
		if mother == father {
			mother = rand.Intn(length)
		} else {
			break
		}
	}
	// fmt.Println("ParentSelection -", "Mother:", mother, "Father:", father)
	return p.TwoIndividual(mother, father)
}

func (p *population) RandomOnePointCrossover(x, y individual) (individual, individual) {
	randOnePoint := rand.Intn(len(x.entities))
	if debug {
		fmt.Println("RandomOnePointCrossover -", "Split at point:", randOnePoint)
		fmt.Println("RandomOnePointCrossover -", "Mother:", x)
		fmt.Println("RandomOnePointCrossover -", "Father:", y)
	}
	tempGene1 := make([]int, 0, len(x.entities))
	tempGene2 := make([]int, 0, len(y.entities))

	tempGene1 = append(tempGene1, x.entities[:randOnePoint]...)
	tempGene2 = append(tempGene2, y.entities[:randOnePoint]...)

	tempGene1 = append(tempGene1, y.entities[randOnePoint:]...)
	tempGene2 = append(tempGene2, x.entities[randOnePoint:]...)

	retGene1 := individual{tempGene1, len(tempGene1), fitness(tempGene1), 0}
	retGene2 := individual{tempGene2, len(tempGene2), fitness(tempGene2), 0}
	if debug {
		fmt.Println("RandomOnePointCrossover -", "Child1:", retGene1)
		fmt.Println("RandomOnePointCrossover -", "Child2:", retGene2)
	}
	return retGene1, retGene2
}

func (i *individual) Entitie(j int) int {
	return i.entities[j]
}

func (i *individual) Entities() []int {
	return i.entities
}

func (i *individual) SetEntities(j []int) {
	i.entities = j
}

func (i *individual) SetEntity(position, value int) {
	i.entities[position] = value
}

func (i *individual) FlipTheBit(pos int) {
	oldval := i.entities[pos]
	newval := 0
	if oldval == 0 {
		newval = 1
	}
	i.SetEntity(pos, newval)
}

func (i *individual) Mutate() {
	length := len(i.entities)
	for j := 0; j < length; j++ {
		if x := rand.Float64(); x < mutationRate {
			i.FlipTheBit(j)
		}
	}
	i.RefreshFitness()
}

func (i *individual) RefreshFitness() {
	i.fitness = fitness(i.entities)
}

// http://stackoverflow.com/questions/15311969/checking-the-equality-of-two-slices
func testEq(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func createRandomValues(length int) []int {
	retArray := make([]int, length)
	for i := 0; i < length; i++ {
		retArray[i] = rand.Intn(2)
	}
	return retArray
}

func fitness(vals []int) int {
	cnt := 0
	for i := 0; i < len(vals); i++ {
		if vals[i] > 0 {
			cnt += 1
		}
	}
	return cnt
}

func main() {
	rand.Seed(time.Now().UTC().UnixNano())
	flag.Parse()

	fmt.Println("Individuals:", amountOfIndividuals)
	fmt.Println("Entities / individual:", amountOfEntities)
	fmt.Println("Desired fitness:", desiredFitness)

	popul := population{}
	popul.Initialize(amountOfIndividuals, amountOfEntities)

	popul.RefreshFitness()
	popul.RefreshAvgFitness()

	fitness := popul.AvgFitness()
	gen := 0

	for fitness < desiredFitness {
		fmt.Println("\n\n", "\bGeneration", gen)
		popul.PrintAll()
		if gen > maxGenerations {
			fmt.Println("We reached the maximal amount of generations and break here.")
			break
		}
		fmt.Println("Breeding for generation", gen+1, "beginns!")
		if debug {
			fmt.Println("Fitness before breeding:", fitness)
		}
		for i := 0; i < childrenPerGeneration; i++ {
			mother, father := popul.ParentSelection()
			chd1, chd2 := popul.RandomOnePointCrossover(mother, father)
			chd1.Mutate()
			if debug && len(chd1.entities) == chd1.fitness {
				fmt.Println("Mutated child1:", chd1, "<===== WINNER!")
			} else if debug {
				fmt.Println("Mutated child1:", chd1)
			}
			chd2.Mutate()
			if debug && len(chd2.entities) == chd2.fitness {
				fmt.Println("Mutated child2:", chd2, "<===== WINNER!")
			} else if debug {
				fmt.Println("Mutated child2:", chd2)
			}
			popul.AppendIndividuals(chd1, chd2)
		}
		popul.KillWeak()
		popul.RefreshAvgFitness()
		popul.RefreshFitness()
		popul.RefreshGeneration(gen)
		gen += 1
		fitness = popul.AvgFitness()
		fmt.Println("Fitness after breeding:", fitness)
		popul.DbWrite()
	}
	fmt.Println("\n\nResult after", gen, "generations:")
	popul.PrintAll()
	fmt.Println("Fitness of", fitness, "is reached")
}

func (p *population) DbWrite() {

	// api.Domain = esHost
	// api.Port = esPort

	// var str string

	for i := range p.individuals {
		conn, err := net.Dial("tcp", esHost+":"+esPort)
		if err != nil {
			// handle error. WUHU!!!
			fmt.Println(err)
		}
		indi := p.Individuals()

		foo, _ := json.Marshal(DbIndividual{indi[i].entities, indi[i].amount, indi[i].fitness, indi[i].generation})
		fmt.Fprintf(conn, string(foo))

		// t := time.Now()
		// res, err := core.Index(esIndex, "foobar", strconv.Itoa(i), map[string]interface{}{"timestamp": strconv.Itoa(t.Nanosecond()), "ttl": 20}, DbIndividual{indi[i].entities, indi[i].amount, indi[i].fitness})
		// t := time.Now()
		// core.IndexBulk(esIndex, "foobar", "1", &t, DbIndividual{indi[i].entities, indi[i].amount, indi[i].fitness})
		conn.Close()
	}
}
