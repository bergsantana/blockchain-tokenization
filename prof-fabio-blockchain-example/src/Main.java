public class Main {

  public static void main(String[] args) {
    Blockchain blockchain = new Blockchain(4);
    blockchain.addBlock(blockchain.newBlock("Tout sur le Bitcoin"));
    blockchain.addBlock(blockchain.newBlock("Sylvain Saurel"));
    blockchain.addBlock(blockchain.newBlock("https://www.toutsurlebitcoin.fr"));
    blockchain.addBlock(blockchain.newBlock("https://www.uea.edu.br"));
    
    System.out.println(blockchain);
    
    System.out.println("Blockchain é válido ? ");    
    if (!blockchain.isBlockChainValid()) {
 	   System.out.println("Não é Válido!!!");
    }else {
    	System.out.println("Sim é Válido!!!");
    }
    

    // add an invalid block to corrupt Blockchain
   
   /* 
   blockchain.addBlock(new Block(15, System.currentTimeMillis(), "aaaabbb", "Block invalid"));
   System.out.println(blockchain);
   
   System.out.println("A blockchain continua é válida ? ");   
   if (!blockchain.isBlockChainValid()) {
	   System.out.println("Não é Válido!!!");
   }*/
  
  }
	
}