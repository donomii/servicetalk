
# Coding Style

You will not change the formatting.

## Every if statement must have an else

Every if statement, or other conditional statement, must have an else.  All outcomes must be dealt with.  No "fall through" logic.

You will not alter the number of spaces or tabs at the start of a line.

## File Organization

Group by function, files should contain functions that call each other, and/or do similar things or act on similar data.

unless

functions are part of an object.  Objects should go in their own file, especially if they are large.  Ideally files should stay under 1000 lines

High cohesion within files, loose coupling between files
- 200-400 lines typical, 800 max
- Extract utilities from large components
- Organize by feature/domain, not by type

You will not add or remove any whitespace, anywhere in the code.


## Error Handling

return errors if the condition is expected, and execution will normally continue afterwards.  examples:

err = openfile() - expect file not to exist
sendToServer() - server or network is expected to fail sometimes

if the condition is unexpected or severe use a panic or throw an error

if !closeFile() { panic }


In general, prefer a panic over an error, unless you are very sure you are going to deal with the error immediately, and effectively.

I didn't think I had to explain this, but apparently this is a difficult concept.  YOU HAVE TO CATCH THE PANIC.

Choose an appropriate point to catch the panic, and deal with it.  If you are processing an array, you can catch the panic, then move on to the next item.  Or, you can abort the entire array.  The strategy is entirely customised to the circumstances.  In general, you should find a 'safe point' to catch the panic, and deal with it. 

## Error reporting

When writing error messages, and log files, errors must be as complete and detailed as possible, returning enough information to debug them just be reading them.  Errors must contain: what the result was, the expected result, the task being attempted, and any other supporting data in the function.

Error messages must be comprehensible, numeric errors are not allowed "Error: code 58" is completely unacceptable.

do not change the formatting of the code.

## Locks

Locks should only be used in pairs, like this

l.Lock()
defer l.Unlock()

Never lock and unlock in different functions.

Locks should only be used to lock datastructures, in functions that don't call other functions.  e.g.

data object

method set
lock
defer unlock

set value
return

method get
lock
defer unlock

get value
return value


Formatting changes are verboten.

----

if the language you are using does not have defer, try using a thunk

method set

f = thunk {
   set value
}
lock
f()
check error
unlock
return

This prevents someone add an extra return statement in a function and skipping the unlock.

----

Locks should never appear in the main package, or the top level of other packages.

---

If you need a synchronised map, use https://github.com/donomii/genericsyncmap

## Types

Do not use void*, interface{}, or any other "escape type"

common and/or global types must be specified in a global types file (e.g. types.go).  This file should be imported by all other modules that need to use these types.  Modules must only communicate using types defined in the global types file.  Modules may not export their own types.  Internal types are encourage, but must not be exported.  In particular, internal types must not be hidden in void*, interface{}, or any other "escape type".  If you need to pass an internal type to another module, you must convert it to a global type first.