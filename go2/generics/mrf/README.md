# Map-Filter-Reduce with Go 2 Generics
This package `mrf` implements the map-filter-reduce pattern. The name means map-reduce-filter, and was probably a mistake. In the future, I will probably change the name to something saner.

## Why?
In the past, people have complained that it is not possible to implement map-filter-reduce without a seperate implementation for each type. Using `interface{}` worked, but provided terrible performance. Generics solve this problem by allowing the compiler to generate the implementations.

I am not building this as a ready-to-use map-filter-reduce package. The goal here is to create a set of realistic package on which the code & concepts in the generics proposal can be tested.

## How it Works - Basics
At its core, this package uses a parameterized interface:
```Go
// Stream is a continuous sequence of values.
type Stream(type E) interface {
	// Get the next value in the stream.
	// When done, io.EOF signals termination of the stream.
	Next() (E, error)
}
```

At a high level, this means that the user just keeps calling next to get more values. When there are none left, it returns `io.EOF` and the user moves on. The user can also legally stop calling `Next` at any time. These properties make it easy to be composed and processed. This is supposed to essentially be the `io.Reader` equivalent for map-filter-reduce.

The intention is for a user to be able to easily implement a stream type for their data source and then use generic processing tools to transform the data. Additionally, generic implementations for common data formats are provided:
* `StreamChannel` - implements `Stream` by reading from a channel
* `StreamSlice` - implements `Stream` by iterating through a slice

### Why use `interface` Instead of `contract`
Both `interface` and `contract` implement polymorphism. However, the two are fundamentally different. Consider the following function:
```Go
func Map (type I, O) (in Stream(I), fn MapFunc(I, O)) Stream(O)
```

If `Stream` were implemented as a `contract` rather than as an `interface`, there would be no way to signify that the return value was used for streaming in code. Additionally, the return value would either need to be unexported or an interface. This would make it substantially more difficult to compose the resulting stream.

Additionally, using the interface makes this much more Go-like. The concept works more or less the same as pre-generics Go, just adding type parameters on top.

## Map, Filter, and Reduce
In order to process the data using a regular operation, the library user implements one of the following function types:

```Go
type MapFunc(type I, O) func(I) (O, error)
type FilterFunc(type E) func(E) (bool, error)
type ReduceFunc(type E) func(E, E) (E, error)
```

These were selected because they:
1. match common function types in current code
2. allow users to process errors resulting from incorrect data or external dependencies
3. include the minimal amount of syntax necessary to accomplish their respective tasks

Originally, function signatures without error returns were used. The error returns were added in order to allow users to report problems with input data.

### Map
```Go
func Map (type I, O) (in Stream(I), fn MapFunc(I, O)) Stream(O)
```

The `Map` implementation accepts a `Stream` and the `MapFunc` and returns a transformed `Stream`. When `Next` is called on the returned `Stream`, it calls `Next` on the underlying `Stream`, then calls the user-provided mapping func to transform the data. This has two important properties:
1. the user does not have to worry about `Stream` termination or semantics
2. the mapping is done on the fly, so the data does not have to fit in memory
3. strong typing is preserved

### Filter
```Go
func Filter (type E) (in Stream(E), fn FilterFunc(E)) Stream(E)
```

The `Filter` implementation is analogous to the `Map` implementation - both accept a `Stream` and return a transformed `Stream`. The resulting `Stream` contains only values for which the `FilterFunc` is matched - where it returns true.

### Reduce
```Go
func Reduce (type E) (in Stream(E), fn ReduceFunc(E)) (E, error)
```

Unlike `Map` and `Filter`, `Reduce` iterates over the passed stream up front. Uses the provided `ReduceFunc` to merge the values in the provided `Stream` together linearly.
* empty stream - returns the zero value of type E
* 1-element stream - returns the element from the stream
* 2-element stream - returns `fn(first, second)`
* 3-element stream - returns `fn(fn(first, second), third)`
* 4-element stream - returns `fn(fn(fn(first, second), third), fourth)`
* etc.

### Error Handling
Any time a `Stream` or `*Func` returns an error, this immediately terminates the map-filter-reduce pipeline. If a `*Func` returns an `io.EOF`, this is replaced with an `io.ErrUnexpectedEOF` in order to avoid confusion with a regular stream termination. Errors are not wrapped.

If a user wishes to use an alternate error-handling model, they can re-implement `Map`, `Filter`, or `Reduce` with their error handling model, while still being able to reuse their `MapFunc`, `FilterFunc`, and `ReduceFunc` implementations.

A possible extension would be to allow `Map` to return a special error indicating that a piece of data should be discarded. This was not implemented in order to avoid conflicts with `Filter`.

## Stream Usage Examples
I created several utilities demonstrating usage of stream outputs:
* `Collect` - collects `Stream` output into a slice in for interoperability with slice-based APIs; operates directly on the `Stream`; more of a core function than an example
* `Sum` - demonstrates using contracts on element types by adding elements from a string together; internally uses `Reduce`
* `Deduplicate` - another demonstration of contracts on element types; uses a map to filter duplicate elements in a stream; internally uses `Filter`
* `Join` - joins strings by a seperator, demonstrates using statically typed `Stream`s; internally uses `Collect` and `strings.Join`

## Concurrency
Given that Go is a concurrent language, it would make sense for the map-filter-reduce implementation to support concurrency. Some constraints are necessary in order to get a decent API:
1. management of goroutines should be hidden from the user
2. no special constraints should apply to the `Stream` interface implementation
3. the user should be able to control how work is broken up

Only one function in this package uses goroutines:
```Go
func ConcMap (type I, O) (in Stream(I), conc uint, fn MapFunc(I, O)) Stream(O)
```

The `ConcMap` is a concurrent version of `Map`. The only change to the function type is adding the `conc` parameter, which controls the number of concurrent jobs. Internally, goroutines are spawned for each element of the stream, up to `conc` at a time. At a high level, the `Next` of the resulting stream is implemented as follows:
1. read inputs and spawn goroutines for each input until the limit of concurrent goroutines is hit or the end of the input `Stream` is reached
2. read from a results channel and return the result or handle the error
3. if there are no goroutines left and no results were collected, return `io.EOF`

Additionally, the results channel is buffered with a capacity equal to the maximum concurrency level. This means that all spawned goroutines can successfully terminate even if the user stops calling `Next`. When an error is encountered, `Next` waits until all goroutines finish before exiting.

### Stream Splitting
Spawning a goroutine per value is not a scalable solution. In order to work around this, you can instead split a stream and then process different parts separately. There are a few ways to do this:

```Go
func Bucketize (type E) (in Stream(E), size uint) Stream([]E)
```
`Bucketize` takes a stream and breaks it into "buckets" of a specified size.


```Go
func SplitChunks (type E) (in Stream(E), chunkSize uint) Stream(Stream(E))
```
`SplitChunks` works like `Bucketize`, except it returns a `Stream` of `Stream`s. On types implementing the `Sub` interface (`StreamSlice`, `Seq`, etc.), it does this by taking a reference to a portion of the original `Stream` rather than copying.

After mapping the streams, a specialized version of `Reduce` can be applied:
```Go
func ReduceAll (type E) (in Stream(Stream(E)), conc uint, fn ReduceFunc(E)) (E, error)
```
This invokes `Reduce` on each stream concurrently, then reduces all of the reduction results together.

#### The Sub Interface
In order to better handle splitting work, a special interface is defined:
```Go
type Sub(type E) interface {
	Stream(E)
	SubStream(uint) (Stream(E), error)
}
```
This allows implementing `Stream` types to be split up faster, by using a specialized implementation to retrieve a sub-section of the `Stream`. For `StreamSlice`, this works by slicing the backing slice, and for `Seq` this creates another `Seq` instance for a sub-sequence.

Unfortunately, this is not actually valid in the current generics draft. There is type ambiguity in the interface embedding - there is nothing to differentiate a function called `Stream` taking an argument of type `E` from an embedding of the parameterized interface type `Stream` using `E` as the type parameter.
